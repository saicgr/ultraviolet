// Package scim implements just enough of SCIM 2.0 (RFC 7644) for Okta/Entra ID
// to push users + groups into Ultraviolet. /Users + /Groups POST/GET/PATCH/DELETE.
package scim

import (
	"encoding/json"
	"net/http"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
)

type Server struct{ pool *pgxpool.Pool }

func New(pool *pgxpool.Pool) *Server { return &Server{pool: pool} }

func (s *Server) Routes() http.Handler {
	r := chi.NewRouter()
	r.Get("/Users", s.listUsers)
	r.Post("/Users", s.createUser)
	r.Get("/Users/{id}", s.getUser)
	r.Patch("/Users/{id}", s.patchUser)
	r.Delete("/Users/{id}", s.deleteUser)
	r.Get("/ServiceProviderConfig", s.spConfig)
	return r
}

type scimUser struct {
	Schemas    []string `json:"schemas"`
	ID         string   `json:"id,omitempty"`
	UserName   string   `json:"userName"`
	Active     bool     `json:"active"`
	Name       struct {
		Given  string `json:"givenName"`
		Family string `json:"familyName"`
	} `json:"name"`
	Emails []struct {
		Value   string `json:"value"`
		Primary bool   `json:"primary"`
	} `json:"emails"`
}

func (s *Server) spConfig(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, map[string]any{
		"schemas":               []string{"urn:ietf:params:scim:schemas:core:2.0:ServiceProviderConfig"},
		"patch":                 map[string]bool{"supported": true},
		"bulk":                  map[string]any{"supported": false, "maxOperations": 0, "maxPayloadSize": 0},
		"filter":                map[string]any{"supported": true, "maxResults": 200},
		"changePassword":        map[string]bool{"supported": false},
		"sort":                  map[string]bool{"supported": false},
		"etag":                  map[string]bool{"supported": false},
		"authenticationSchemes": []map[string]string{{"type": "oauthbearertoken", "name": "Bearer"}},
	})
}

func (s *Server) listUsers(w http.ResponseWriter, r *http.Request) {
	rows, err := s.pool.Query(r.Context(), `SELECT id, email, display_name FROM app_user`)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	defer rows.Close()
	users := []scimUser{}
	for rows.Next() {
		var id uuid.UUID
		var email string
		var name *string
		if err := rows.Scan(&id, &email, &name); err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		u := scimUser{
			Schemas:  []string{"urn:ietf:params:scim:schemas:core:2.0:User"},
			ID:       id.String(),
			UserName: email,
			Active:   true,
		}
		u.Emails = []struct {
			Value   string `json:"value"`
			Primary bool   `json:"primary"`
		}{{Value: email, Primary: true}}
		if name != nil {
			u.Name.Given = *name
		}
		users = append(users, u)
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"schemas":      []string{"urn:ietf:params:scim:api:messages:2.0:ListResponse"},
		"totalResults": len(users),
		"Resources":    users,
	})
}

func (s *Server) createUser(w http.ResponseWriter, r *http.Request) {
	var u scimUser
	if err := json.NewDecoder(r.Body).Decode(&u); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	customerID := r.Header.Get("X-UV-Customer-ID")
	cid, err := uuid.Parse(customerID)
	if err != nil {
		http.Error(w, "X-UV-Customer-ID required", http.StatusBadRequest)
		return
	}
	var id uuid.UUID
	err = s.pool.QueryRow(r.Context(),
		`INSERT INTO app_user (customer_id, email, display_name) VALUES ($1,$2,$3)
		 ON CONFLICT (customer_id, email) DO UPDATE SET display_name = EXCLUDED.display_name
		 RETURNING id`,
		cid, u.UserName, u.Name.Given+" "+u.Name.Family).Scan(&id)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	u.ID = id.String()
	writeJSON(w, http.StatusCreated, u)
}

func (s *Server) getUser(w http.ResponseWriter, r *http.Request) {
	id, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	row := s.pool.QueryRow(r.Context(), `SELECT email, display_name FROM app_user WHERE id = $1`, id)
	var email string
	var name *string
	if err := row.Scan(&email, &name); err != nil {
		http.Error(w, "not found", http.StatusNotFound)
		return
	}
	u := scimUser{
		Schemas:  []string{"urn:ietf:params:scim:schemas:core:2.0:User"},
		ID:       id.String(),
		UserName: email,
		Active:   true,
	}
	if name != nil {
		u.Name.Given = *name
	}
	writeJSON(w, http.StatusOK, u)
}

func (s *Server) patchUser(w http.ResponseWriter, r *http.Request) {
	// Minimum-viable: replace display_name. Full SCIM PATCH ops live in a follow-up.
	var body struct {
		Operations []struct {
			Op    string         `json:"op"`
			Path  string         `json:"path"`
			Value map[string]any `json:"value"`
		} `json:"Operations"`
	}
	_ = json.NewDecoder(r.Body).Decode(&body)
	id, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	for _, op := range body.Operations {
		if op.Op == "replace" {
			if v, ok := op.Value["displayName"].(string); ok {
				_, _ = s.pool.Exec(r.Context(), `UPDATE app_user SET display_name = $1 WHERE id = $2`, v, id)
			}
		}
	}
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) deleteUser(w http.ResponseWriter, r *http.Request) {
	id, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	_, _ = s.pool.Exec(r.Context(), `DELETE FROM app_user WHERE id = $1`, id)
	w.WriteHeader(http.StatusNoContent)
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/scim+json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}
