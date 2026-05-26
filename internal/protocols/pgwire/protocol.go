// Package pgwire implements the Postgres v3 wire protocol — enough of it to act as
// a passthrough proxy. See docs/architecture/pg-wire-protocol.md and
// .claude/skills/pg-wire-reference/SKILL.md for message reference.
package pgwire

import (
	"bufio"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
)

// Frontend → Backend message types (first byte of typed messages).
const (
	MsgQuery       byte = 'Q'
	MsgParse       byte = 'P'
	MsgBind        byte = 'B'
	MsgExecute     byte = 'E'
	MsgDescribe    byte = 'D'
	MsgSync        byte = 'S'
	MsgFlush       byte = 'H'
	MsgClose       byte = 'C'
	MsgTerminate   byte = 'X'
	MsgPasswordMsg byte = 'p'
	MsgCopyData    byte = 'd'
	MsgCopyDone    byte = 'c'
)

// Backend → Frontend message types.
const (
	BMsgAuthentication    byte = 'R'
	BMsgBackendKeyData    byte = 'K'
	BMsgBindComplete      byte = '2'
	BMsgCloseComplete     byte = '3'
	BMsgCommandComplete   byte = 'C'
	BMsgDataRow           byte = 'D'
	BMsgEmptyQueryResp    byte = 'I'
	BMsgErrorResponse     byte = 'E'
	BMsgNoData            byte = 'n'
	BMsgNoticeResp        byte = 'N'
	BMsgParameterDesc     byte = 't'
	BMsgParameterStatus   byte = 'S'
	BMsgParseComplete     byte = '1'
	BMsgReadyForQuery     byte = 'Z'
	BMsgRowDescription    byte = 'T'
	BMsgPortalSuspended   byte = 's'
	BMsgNegotiateProtocol byte = 'v'
)

// Authentication request subtypes.
const (
	AuthOK                = 0
	AuthCleartextPassword = 3
	AuthMD5Password       = 5
	AuthSASL              = 10
)

// Common Postgres OIDs (subset). See pg_type.h.
const (
	OIDBool        = 16
	OIDBytea       = 17
	OIDInt8        = 20
	OIDInt2        = 21
	OIDInt4        = 23
	OIDText        = 25
	OIDFloat4      = 700
	OIDFloat8      = 701
	OIDVarchar     = 1043
	OIDDate        = 1082
	OIDTime        = 1083
	OIDTimestamp   = 1114
	OIDTimestamptz = 1184
	OIDNumeric     = 1700
	OIDJSON        = 114
	OIDJSONB       = 3802
	OIDUUID        = 2950
)

// Field describes one column of a result set (RowDescription entry).
type Field struct {
	Name         string
	TableOID     uint32
	ColumnAttrNo int16
	TypeOID      uint32
	TypeSize     int16
	TypeModifier int32
	Format       int16 // 0 text, 1 binary
}

// StartupMessage parsed from the client.
type StartupMessage struct {
	ProtocolVersion uint32
	Parameters      map[string]string // "user", "database", "application_name", ...
	CancelKey       *CancelKey        // populated only when this was a CancelRequest
}

// IsSSLRequest = magic 80877103 (1234<<16 | 5679)
const sslRequestMagic uint32 = 80877103

// IsCancelRequest = magic 80877102
const cancelRequestMagic uint32 = 80877102

// CancelKey is the (pid, secret) pair extracted from a CancelRequest.
type CancelKey struct {
	PID    int32
	Secret int32
}

// ReadStartupMessage reads either a real startup message, an SSLRequest, or a CancelRequest.
// Returns (msg, isSSL, isCancel, err). When isCancel=true, msg.CancelKey is set.
func ReadStartupMessage(r *bufio.Reader) (*StartupMessage, bool, bool, error) {
	var lenBuf [4]byte
	if _, err := io.ReadFull(r, lenBuf[:]); err != nil {
		return nil, false, false, err
	}
	length := int(binary.BigEndian.Uint32(lenBuf[:]))
	if length < 8 || length > 1<<20 {
		return nil, false, false, fmt.Errorf("startup length out of range: %d", length)
	}
	body := make([]byte, length-4)
	if _, err := io.ReadFull(r, body); err != nil {
		return nil, false, false, err
	}
	version := binary.BigEndian.Uint32(body[:4])
	if version == sslRequestMagic {
		return nil, true, false, nil
	}
	if version == cancelRequestMagic {
		if len(body) < 12 {
			return nil, false, true, fmt.Errorf("cancel request truncated")
		}
		key := &CancelKey{
			PID:    int32(binary.BigEndian.Uint32(body[4:8])),
			Secret: int32(binary.BigEndian.Uint32(body[8:12])),
		}
		return &StartupMessage{CancelKey: key}, false, true, nil
	}
	params := map[string]string{}
	rest := body[4:]
	for len(rest) > 1 {
		k, n := readCString(rest)
		if n == 0 {
			break
		}
		rest = rest[n:]
		v, n := readCString(rest)
		rest = rest[n:]
		if k == "" {
			break
		}
		params[k] = v
	}
	return &StartupMessage{ProtocolVersion: version, Parameters: params}, false, false, nil
}

// Frame is a typed message (post-startup).
type Frame struct {
	Type byte
	Body []byte
}

// ReadFrame reads one typed message from r.
func ReadFrame(r *bufio.Reader) (Frame, error) {
	t, err := r.ReadByte()
	if err != nil {
		return Frame{}, err
	}
	var lenBuf [4]byte
	if _, err := io.ReadFull(r, lenBuf[:]); err != nil {
		return Frame{}, err
	}
	length := int(binary.BigEndian.Uint32(lenBuf[:]))
	if length < 4 || length > 1<<24 {
		return Frame{}, fmt.Errorf("frame length out of range: %d", length)
	}
	body := make([]byte, length-4)
	if _, err := io.ReadFull(r, body); err != nil {
		return Frame{}, err
	}
	return Frame{Type: t, Body: body}, nil
}

// ----- Writers -----

type Writer struct {
	w   *bufio.Writer
	buf []byte
}

func NewWriter(w io.Writer) *Writer {
	return &Writer{w: bufio.NewWriterSize(w, 32*1024)}
}

func (w *Writer) Flush() error { return w.w.Flush() }

func (w *Writer) writeFrame(t byte, body []byte) error {
	var hdr [5]byte
	hdr[0] = t
	binary.BigEndian.PutUint32(hdr[1:], uint32(len(body)+4))
	if _, err := w.w.Write(hdr[:]); err != nil {
		return err
	}
	_, err := w.w.Write(body)
	return err
}

// AuthenticationOk
func (w *Writer) WriteAuthOK() error {
	body := make([]byte, 4)
	binary.BigEndian.PutUint32(body, AuthOK)
	return w.writeFrame(BMsgAuthentication, body)
}

func (w *Writer) WriteAuthCleartext() error {
	body := make([]byte, 4)
	binary.BigEndian.PutUint32(body, AuthCleartextPassword)
	return w.writeFrame(BMsgAuthentication, body)
}

func (w *Writer) WriteParameterStatus(name, value string) error {
	body := append([]byte{}, []byte(name)...)
	body = append(body, 0)
	body = append(body, []byte(value)...)
	body = append(body, 0)
	return w.writeFrame(BMsgParameterStatus, body)
}

// BackendKeyData — process id + secret key. Used for cancel; we send dummies.
func (w *Writer) WriteBackendKeyData(pid, secret int32) error {
	body := make([]byte, 8)
	binary.BigEndian.PutUint32(body, uint32(pid))
	binary.BigEndian.PutUint32(body[4:], uint32(secret))
	return w.writeFrame(BMsgBackendKeyData, body)
}

// ReadyForQuery — status 'I' idle, 'T' txn, 'E' failed txn.
func (w *Writer) WriteReadyForQuery(status byte) error {
	return w.writeFrame(BMsgReadyForQuery, []byte{status})
}

// RowDescription. PG hard-limits column count to 1664; reject pathological inputs.
const MaxColumns = 1664

func (w *Writer) WriteRowDescription(fields []Field) error {
	if len(fields) > MaxColumns {
		return fmt.Errorf("RowDescription: %d columns exceeds PG limit %d", len(fields), MaxColumns)
	}
	buf := make([]byte, 2)
	binary.BigEndian.PutUint16(buf, uint16(len(fields)))
	for _, f := range fields {
		buf = append(buf, []byte(f.Name)...)
		buf = append(buf, 0)
		var tmp [18]byte
		binary.BigEndian.PutUint32(tmp[0:], f.TableOID)
		binary.BigEndian.PutUint16(tmp[4:], uint16(f.ColumnAttrNo))
		binary.BigEndian.PutUint32(tmp[6:], f.TypeOID)
		binary.BigEndian.PutUint16(tmp[10:], uint16(f.TypeSize))
		binary.BigEndian.PutUint32(tmp[12:], uint32(f.TypeModifier))
		binary.BigEndian.PutUint16(tmp[16:], uint16(f.Format))
		buf = append(buf, tmp[:]...)
	}
	return w.writeFrame(BMsgRowDescription, buf)
}

// WriteDataRow writes a single row. Each value is either nil (NULL) or its raw text encoding.
func (w *Writer) WriteDataRow(values [][]byte) error {
	size := 2
	for _, v := range values {
		size += 4
		if v != nil {
			size += len(v)
		}
	}
	buf := make([]byte, 0, size)
	buf = appendU16(buf, uint16(len(values)))
	for _, v := range values {
		if v == nil {
			buf = appendI32(buf, -1)
			continue
		}
		buf = appendI32(buf, int32(len(v)))
		buf = append(buf, v...)
	}
	return w.writeFrame(BMsgDataRow, buf)
}

func (w *Writer) WriteCommandComplete(tag string) error {
	body := append([]byte(tag), 0)
	return w.writeFrame(BMsgCommandComplete, body)
}

func (w *Writer) WriteEmptyQueryResponse() error {
	return w.writeFrame(BMsgEmptyQueryResp, nil)
}

// WriteParameterDescription emits ParameterDescription (`t`) for the bound
// statement: a count followed by the parameter type OIDs. Required by drivers
// (e.g. JDBC, dbt-postgres) that issue Describe('S', name) before Bind.
func (w *Writer) WriteParameterDescription(paramOIDs []uint32) error {
	buf := make([]byte, 2)
	binary.BigEndian.PutUint16(buf, uint16(len(paramOIDs)))
	for _, o := range paramOIDs {
		var tmp [4]byte
		binary.BigEndian.PutUint32(tmp[:], o)
		buf = append(buf, tmp[:]...)
	}
	return w.writeFrame(BMsgParameterDesc, buf)
}

func (w *Writer) WriteParseComplete() error   { return w.writeFrame(BMsgParseComplete, nil) }
func (w *Writer) WriteBindComplete() error    { return w.writeFrame(BMsgBindComplete, nil) }
func (w *Writer) WriteCloseComplete() error   { return w.writeFrame(BMsgCloseComplete, nil) }
func (w *Writer) WriteNoData() error          { return w.writeFrame(BMsgNoData, nil) }
func (w *Writer) WritePortalSuspended() error { return w.writeFrame(BMsgPortalSuspended, nil) }

// ErrorResponse — see PG docs for field codes.
type ErrorField struct {
	Code byte
	Val  string
}

func (w *Writer) WriteErrorResponse(severity, sqlstate, message string, extras ...ErrorField) error {
	var body []byte
	body = appendField(body, 'S', severity)
	body = appendField(body, 'V', severity)
	body = appendField(body, 'C', sqlstate)
	body = appendField(body, 'M', message)
	for _, e := range extras {
		body = appendField(body, e.Code, e.Val)
	}
	body = append(body, 0)
	return w.writeFrame(BMsgErrorResponse, body)
}

// ----- helpers -----

func appendField(b []byte, code byte, v string) []byte {
	b = append(b, code)
	b = append(b, []byte(v)...)
	b = append(b, 0)
	return b
}

func appendU16(b []byte, v uint16) []byte {
	var tmp [2]byte
	binary.BigEndian.PutUint16(tmp[:], v)
	return append(b, tmp[:]...)
}

func appendI32(b []byte, v int32) []byte {
	var tmp [4]byte
	binary.BigEndian.PutUint32(tmp[:], uint32(v))
	return append(b, tmp[:]...)
}

func readCString(b []byte) (string, int) {
	for i, c := range b {
		if c == 0 {
			return string(b[:i]), i + 1
		}
	}
	return "", 0
}

// ErrTerminate signals a clean client Terminate message.
var ErrTerminate = errors.New("client terminate")
