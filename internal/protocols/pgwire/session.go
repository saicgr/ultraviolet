package pgwire

import (
	"bufio"
	"context"
	"crypto/rand"
	"crypto/tls"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"net"
	"strings"
	"sync"
	"time"

	"github.com/rs/zerolog"

	"github.com/ultraviolet-dev/ultraviolet/internal/branding"
)

// randomBackendKey returns a non-zero (pid, secret) pair so cancel-key collisions
// across sessions are astronomically unlikely. Used in BackendKeyData.
func randomBackendKey() (int32, int32) {
	var b [8]byte
	if _, err := rand.Read(b[:]); err != nil {
		return 1, 1
	}
	return int32(binary.BigEndian.Uint32(b[0:4]) & 0x7fffffff), int32(binary.BigEndian.Uint32(b[4:8]) & 0x7fffffff)
}

// QueryHandler is the proxy's query backend. The pgwire layer is protocol-only;
// classification + routing live in internal/router and execution in internal/connectors
// + internal/workers. This indirection lets us unit-test pgwire with a fake handler.
type QueryHandler interface {
	HandleSimpleQuery(ctx context.Context, sess *Session, sql string, w *Writer) error
	HandleExtendedExecute(ctx context.Context, sess *Session, p *Portal, w *Writer) error
}

// SQLStateError optionally exposes a Postgres SQLSTATE code from an error returned
// by a connector / handler. The pgwire layer checks for this interface before
// falling back to XX000 (internal_error). Implemented by connectors via
// MapWarehouseError → SQLState.
type SQLStateError interface {
	SQLState() string
}

// errSQLState walks the error chain looking for any error implementing SQLState();
// returns "XX000" if none found.
func errSQLState(err error) string {
	for e := err; e != nil; {
		if s, ok := e.(SQLStateError); ok {
			return s.SQLState()
		}
		type unwrapper interface{ Unwrap() error }
		u, ok := e.(unwrapper)
		if !ok {
			break
		}
		e = u.Unwrap()
	}
	return "XX000"
}

// Session is the per-connection state.
type Session struct {
	Identity       *SessionIdentity
	Application    string
	ClientEncoding string
	StartedAt      time.Time
	// Extended-protocol state
	preparedStatements map[string]*PreparedStatement
	portals            map[string]*Portal
	// Cancel-key wiring. cancelKey is sent in BackendKeyData; ctxCancel is the
	// cancel fn that fireCancel triggers when a CancelRequest arrives.
	cancelKey *CancelKey
	ctx       context.Context
	ctxCancel context.CancelFunc
}

type PreparedStatement struct {
	Name      string
	SQL       string
	ParamOIDs []uint32
}

type Portal struct {
	Name              string
	Statement         *PreparedStatement
	ParamFormatCodes  []int16
	ParamValues       [][]byte
	ResultFormatCodes []int16
}

// Server is a TCP listener that speaks PG v3.
type Server struct {
	Addr        string
	TLSConfig   *tls.Config // optional
	Auth        *Authenticator
	Handler     QueryHandler
	Log         zerolog.Logger
	MaxInflight int           // 0 → 4096 default; bounds total open client conns
	DrainGrace  time.Duration // 0 → 30s default; in-flight conns drain on shutdown

	wg      sync.WaitGroup
	sem     chan struct{}
	semOnce sync.Once

	// Cancel-key registry. PG sends CancelRequest on a fresh connection with
	// (pid, secret); we look up the matching session and call its cancelFn.
	cancelMu  sync.Mutex
	cancelFns map[uint64]context.CancelFunc // key = uint64(pid)<<32 | secret
}

func (s *Server) registerCancelKey(pid, secret int32, fn context.CancelFunc) {
	s.cancelMu.Lock()
	defer s.cancelMu.Unlock()
	if s.cancelFns == nil {
		s.cancelFns = map[uint64]context.CancelFunc{}
	}
	s.cancelFns[cancelKey(pid, secret)] = fn
}

func (s *Server) unregisterCancelKey(pid, secret int32) {
	s.cancelMu.Lock()
	defer s.cancelMu.Unlock()
	delete(s.cancelFns, cancelKey(pid, secret))
}

func (s *Server) fireCancel(pid, secret int32) {
	s.cancelMu.Lock()
	fn := s.cancelFns[cancelKey(pid, secret)]
	s.cancelMu.Unlock()
	if fn != nil {
		fn()
	}
}

func cancelKey(pid, secret int32) uint64 {
	return uint64(uint32(pid))<<32 | uint64(uint32(secret))
}

func (s *Server) initSem() {
	s.semOnce.Do(func() {
		n := s.MaxInflight
		if n <= 0 {
			n = 4096
		}
		s.sem = make(chan struct{}, n)
	})
}

func (s *Server) ListenAndServe(ctx context.Context) error {
	s.initSem()
	ln, err := net.Listen("tcp", s.Addr)
	if err != nil {
		return fmt.Errorf("listen %s: %w", s.Addr, err)
	}
	s.Log.Info().Str("addr", s.Addr).Msg("pgwire listening")
	go func() {
		<-ctx.Done()
		_ = ln.Close()
	}()
	for {
		c, err := ln.Accept()
		if err != nil {
			if ctx.Err() != nil {
				// Graceful drain — wait for in-flight conns up to DrainGrace.
				grace := s.DrainGrace
				if grace == 0 {
					grace = 30 * time.Second
				}
				done := make(chan struct{})
				go func() { s.wg.Wait(); close(done) }()
				select {
				case <-done:
				case <-time.After(grace):
					s.Log.Warn().Dur("grace", grace).Msg("pgwire drain timeout — forcing close")
				}
				return nil
			}
			s.Log.Warn().Err(err).Msg("accept")
			continue
		}
		select {
		case s.sem <- struct{}{}:
		default:
			s.Log.Warn().Str("remote", c.RemoteAddr().String()).Msg("pgwire conn rejected (over MaxInflight)")
			_ = c.Close()
			continue
		}
		s.wg.Add(1)
		go func(conn net.Conn) {
			defer s.wg.Done()
			defer func() { <-s.sem }()
			s.serveConn(ctx, conn)
		}(c)
	}
}

func (s *Server) serveConn(ctx context.Context, c net.Conn) {
	defer c.Close()
	log := s.Log.With().Str("remote", c.RemoteAddr().String()).Logger()
	r := bufio.NewReaderSize(c, 32*1024)

	// Optional SSLRequest negotiation.
	for {
		msg, isSSL, isCancel, err := ReadStartupMessage(r)
		if err != nil {
			log.Debug().Err(err).Msg("startup read")
			return
		}
		if isCancel {
			if msg != nil && msg.CancelKey != nil {
				log.Debug().Int32("pid", msg.CancelKey.PID).Msg("cancel request received")
				s.fireCancel(msg.CancelKey.PID, msg.CancelKey.Secret)
			}
			return
		}
		if isSSL {
			if s.TLSConfig != nil {
				if _, err := c.Write([]byte{'S'}); err != nil {
					return
				}
				tc := tls.Server(c, s.TLSConfig)
				if err := tc.Handshake(); err != nil {
					log.Debug().Err(err).Msg("tls handshake")
					return
				}
				c = tc
				r = bufio.NewReaderSize(c, 32*1024)
				continue
			}
			if _, err := c.Write([]byte{'N'}); err != nil {
				return
			}
			continue
		}
		s.handleSession(ctx, c, r, msg, log)
		return
	}
}

func (s *Server) handleSession(ctx context.Context, c net.Conn, r *bufio.Reader, startup *StartupMessage, log zerolog.Logger) {
	w := NewWriter(c)
	user := startup.Parameters["user"]
	database := startup.Parameters["database"]
	if database == "" {
		database = user
	}

	if s.Auth == nil {
		// Smoke-test mode: no DB, accept anyone, echo SELECT 1. Used by Phase-1 step 1 boot.
		if err := s.completeStartup(w, &Session{
			Identity:           &SessionIdentity{CustomerSlug: user, WarehouseType: stripWarehouseSuffix(database)},
			StartedAt:          time.Now(),
			preparedStatements: map[string]*PreparedStatement{},
			portals:            map[string]*Portal{},
		}); err != nil {
			return
		}
		s.runQueryLoop(ctx, r, w, &Session{
			Identity:           &SessionIdentity{CustomerSlug: user},
			preparedStatements: map[string]*PreparedStatement{},
			portals:            map[string]*Portal{},
		}, log)
		return
	}

	// Cleartext password challenge — API key arrives on the next message.
	if err := w.WriteAuthCleartext(); err != nil {
		return
	}
	if err := w.Flush(); err != nil {
		return
	}
	pwFrame, err := ReadFrame(r)
	if err != nil || pwFrame.Type != MsgPasswordMsg {
		_ = w.WriteErrorResponse("FATAL", "28000", "expected password message")
		_ = w.Flush()
		return
	}
	apiKey := strings.TrimRight(string(pwFrame.Body), "\x00")
	identity, err := s.Auth.Authenticate(ctx, apiKey, database)
	if err != nil {
		_ = w.WriteErrorResponse("FATAL", "28P01", "authentication failed")
		_ = w.Flush()
		log.Warn().Err(err).Str("user", user).Str("db", database).Msg("auth failed")
		return
	}
	sess := &Session{
		Identity:           identity,
		Application:        startup.Parameters["application_name"],
		ClientEncoding:     startup.Parameters["client_encoding"],
		StartedAt:          time.Now(),
		preparedStatements: map[string]*PreparedStatement{},
		portals:            map[string]*Portal{},
	}
	if err := s.completeStartup(w, sess); err != nil {
		return
	}
	log.Info().Str("customer", identity.CustomerSlug).Str("warehouse", identity.WarehouseType).Msg("session start")
	s.runQueryLoop(ctx, r, w, sess, log)
}

func stripWarehouseSuffix(db string) string {
	for _, w := range []string{"bigquery", "snowflake", "databricks"} {
		if strings.HasSuffix(db, "_"+w) {
			return w
		}
	}
	return ""
}

func (s *Server) completeStartup(w *Writer, sess *Session) error {
	if err := w.WriteAuthOK(); err != nil {
		return err
	}
	for k, v := range map[string]string{
		"server_version":              branding.ServerVersion(),
		"server_encoding":             "UTF8",
		"client_encoding":             "UTF8",
		"DateStyle":                   "ISO, MDY",
		"TimeZone":                    "UTC",
		"integer_datetimes":           "on",
		"standard_conforming_strings": "on",
		"application_name":            sess.Application,
	} {
		if err := w.WriteParameterStatus(k, v); err != nil {
			return err
		}
	}
	pid, secret := randomBackendKey()
	// Register cancel key with a per-session ctx; runQueryLoop wires the cancel
	// fn into the running query's context so a CancelRequest from a sibling
	// connection actually aborts the in-flight statement.
	sessCtx, cancel := context.WithCancel(context.Background())
	s.registerCancelKey(pid, secret, cancel)
	sess.cancelKey = &CancelKey{PID: pid, Secret: secret}
	sess.ctxCancel = cancel
	sess.ctx = sessCtx
	if err := w.WriteBackendKeyData(pid, secret); err != nil {
		return err
	}
	if err := w.WriteReadyForQuery('I'); err != nil {
		return err
	}
	return w.Flush()
}

func (s *Server) runQueryLoop(ctx context.Context, r *bufio.Reader, w *Writer, sess *Session, log zerolog.Logger) {
	for {
		f, err := ReadFrame(r)
		if err != nil {
			if !errors.Is(err, io.EOF) {
				log.Debug().Err(err).Msg("read frame")
			}
			return
		}
		switch f.Type {
		case MsgQuery:
			sql := strings.TrimRight(string(f.Body), "\x00")
			if strings.TrimSpace(sql) == "" {
				_ = w.WriteEmptyQueryResponse()
				_ = w.WriteReadyForQuery('I')
				_ = w.Flush()
				continue
			}
			if err := s.dispatch(ctx, sess, sql, w); err != nil {
				_ = w.WriteErrorResponse("ERROR", errSQLState(err), err.Error())
			}
			_ = w.WriteReadyForQuery('I')
			_ = w.Flush()

		case MsgParse:
			if err := s.handleParse(sess, f.Body, w); err != nil {
				_ = w.WriteErrorResponse("ERROR", "42601", err.Error())
				_ = w.Flush()
			}

		case MsgBind:
			if err := s.handleBind(sess, f.Body, w); err != nil {
				_ = w.WriteErrorResponse("ERROR", "42601", err.Error())
				_ = w.Flush()
			}

		case MsgDescribe:
			if err := s.handleDescribe(sess, f.Body, w); err != nil {
				_ = w.WriteErrorResponse("ERROR", "42601", err.Error())
			}
			_ = w.Flush()

		case MsgExecute:
			if err := s.handleExecute(ctx, sess, f.Body, w); err != nil {
				_ = w.WriteErrorResponse("ERROR", errSQLState(err), err.Error())
				_ = w.Flush()
			}

		case MsgSync:
			_ = w.WriteReadyForQuery('I')
			_ = w.Flush()

		case MsgClose:
			_ = w.WriteCloseComplete()
			_ = w.Flush()

		case MsgFlush:
			_ = w.Flush()

		case MsgTerminate:
			if sess.cancelKey != nil {
				s.unregisterCancelKey(sess.cancelKey.PID, sess.cancelKey.Secret)
			}
			if sess.ctxCancel != nil {
				sess.ctxCancel()
			}
			return

		default:
			log.Debug().Uint8("type", f.Type).Msg("unhandled frame")
		}
	}
}

func (s *Server) dispatch(ctx context.Context, sess *Session, sql string, w *Writer) error {
	if s.Handler == nil {
		// No handler wired — return a hardcoded `SELECT 1` so the smoke test passes.
		return WriteHardcodedSelectOne(w, sql)
	}
	cctx, cancel := context.WithTimeout(mergeCtx(ctx, sess.ctx), 30*time.Second)
	defer cancel()
	return s.Handler.HandleSimpleQuery(cctx, sess, sql, w)
}

// mergeCtx returns a context that's cancelled if either parent is. Used so a
// CancelRequest (which cancels sess.ctx) aborts the in-flight query without
// killing the connection.
func mergeCtx(a, b context.Context) context.Context {
	if b == nil {
		return a
	}
	merged, cancel := context.WithCancel(a)
	go func() {
		select {
		case <-a.Done():
		case <-b.Done():
			cancel()
		}
	}()
	return merged
}

// WriteHardcodedSelectOne fulfills the Phase-1 step-1 contract:
// `SELECT 1` returns one row with column "?column?" int4 = 1; everything else
// returns CommandComplete with tag matching the verb.
func WriteHardcodedSelectOne(w *Writer, sql string) error {
	t := strings.ToUpper(strings.TrimSpace(sql))
	if strings.HasPrefix(t, "SELECT") {
		fields := []Field{{Name: "?column?", TypeOID: OIDInt4, TypeSize: 4, Format: 0}}
		if err := w.WriteRowDescription(fields); err != nil {
			return err
		}
		if err := w.WriteDataRow([][]byte{[]byte("1")}); err != nil {
			return err
		}
		return w.WriteCommandComplete("SELECT 1")
	}
	return w.WriteCommandComplete("OK")
}

// ----- Extended protocol -----

func (s *Server) handleParse(sess *Session, body []byte, w *Writer) error {
	name, n := readCString(body)
	body = body[n:]
	sql, n := readCString(body)
	body = body[n:]
	if len(body) < 2 {
		return fmt.Errorf("parse: short body")
	}
	pCount := int(binary.BigEndian.Uint16(body[:2]))
	body = body[2:]
	if pCount > 0 && len(body) < 4*pCount {
		return fmt.Errorf("parse: short param-OID list")
	}
	oids := make([]uint32, pCount)
	for i := 0; i < pCount; i++ {
		oids[i] = binary.BigEndian.Uint32(body[4*i : 4*i+4])
	}
	sess.preparedStatements[name] = &PreparedStatement{Name: name, SQL: sql, ParamOIDs: oids}
	return w.WriteParseComplete()
}

// handleDescribe responds to Describe(target, name).
//   - target 'S' → ParameterDescription + RowDescription/NoData
//   - target 'P' → RowDescription/NoData for an open portal
//
// We don't introspect the underlying SQL to compute a real RowDescription yet
// (warehouse-side preparation lives in connectors/), so result-shape Describes
// fall through to NoData. ParameterDescription uses the OIDs captured at Parse
// time, satisfying drivers that block on it.
func (s *Server) handleDescribe(sess *Session, body []byte, w *Writer) error {
	if len(body) < 1 {
		return fmt.Errorf("describe: empty body")
	}
	target := body[0]
	name, _ := readCString(body[1:])
	switch target {
	case 'S':
		stmt := sess.preparedStatements[name]
		oids := []uint32{}
		if stmt != nil {
			oids = stmt.ParamOIDs
		}
		if err := w.WriteParameterDescription(oids); err != nil {
			return err
		}
	}
	return w.WriteNoData()
}

func (s *Server) handleBind(sess *Session, body []byte, w *Writer) error {
	portalName, n := readCString(body)
	body = body[n:]
	stmtName, n := readCString(body)
	body = body[n:]
	stmt := sess.preparedStatements[stmtName]
	if stmt == nil {
		return fmt.Errorf("unknown prepared statement %q", stmtName)
	}
	if len(body) < 2 {
		return fmt.Errorf("bind: short body")
	}
	pfc := int(binary.BigEndian.Uint16(body[:2]))
	body = body[2:]
	pFormats := make([]int16, pfc)
	for i := 0; i < pfc && len(body) >= 2; i++ {
		pFormats[i] = int16(binary.BigEndian.Uint16(body[:2]))
		body = body[2:]
	}
	if len(body) < 2 {
		return fmt.Errorf("bind: short param count")
	}
	pc := int(binary.BigEndian.Uint16(body[:2]))
	body = body[2:]
	pValues := make([][]byte, pc)
	for i := 0; i < pc; i++ {
		if len(body) < 4 {
			return fmt.Errorf("bind: short param length")
		}
		l := int32(binary.BigEndian.Uint32(body[:4]))
		body = body[4:]
		if l == -1 {
			pValues[i] = nil
			continue
		}
		if int(l) > len(body) {
			return fmt.Errorf("bind: param %d truncated", i)
		}
		pValues[i] = append([]byte(nil), body[:l]...)
		body = body[l:]
	}
	if len(body) < 2 {
		return fmt.Errorf("bind: short result-format list")
	}
	rfc := int(binary.BigEndian.Uint16(body[:2]))
	body = body[2:]
	rFormats := make([]int16, rfc)
	for i := 0; i < rfc && len(body) >= 2; i++ {
		rFormats[i] = int16(binary.BigEndian.Uint16(body[:2]))
		body = body[2:]
	}
	sess.portals[portalName] = &Portal{
		Name:              portalName,
		Statement:         stmt,
		ParamFormatCodes:  pFormats,
		ParamValues:       pValues,
		ResultFormatCodes: rFormats,
	}
	return w.WriteBindComplete()
}

func (s *Server) handleExecute(ctx context.Context, sess *Session, body []byte, w *Writer) error {
	portalName, _ := readCString(body)
	p := sess.portals[portalName]
	if p == nil {
		return fmt.Errorf("unknown portal %q", portalName)
	}
	if s.Handler == nil {
		if err := WriteHardcodedSelectOne(w, p.Statement.SQL); err != nil {
			return err
		}
		return nil
	}
	cctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	return s.Handler.HandleExtendedExecute(cctx, sess, p, w)
}
