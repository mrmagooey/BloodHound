// Copyright 2023 Specter Ops, Inc.
//
// Licensed under the Apache License, Version 2.0
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//     http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.
//
// SPDX-License-Identifier: Apache-2.0

package middleware_test

import (
	"encoding/base64"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gorilla/mux"
	"github.com/specterops/bloodhound/cmd/api/src/api/middleware"
	"github.com/stretchr/testify/require"
	"golang.org/x/crypto/bcrypt"
)

// newBasicAuthRouter builds a mux.Router with BasicAuthMiddleware applied and a
// simple sentinel handler registered at GET /test.
func newBasicAuthRouter(username string, hashedPassword []byte) *mux.Router {
	r := mux.NewRouter()
	r.Use(middleware.BasicAuthMiddleware(username, hashedPassword))
	r.HandleFunc("/test", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		w.Write([]byte("ok"))
	}).Methods(http.MethodGet)
	return r
}

// hashPassword is a test helper that bcrypt-hashes a plaintext password using
// the minimum cost so tests run quickly.
func hashPassword(t *testing.T, plaintext string) []byte {
	t.Helper()
	h, err := bcrypt.GenerateFromPassword([]byte(plaintext), bcrypt.MinCost)
	require.NoError(t, err)
	return h
}

// basicAuthHeader returns a well-formed "Authorization: Basic …" header value.
func basicAuthHeader(username, password string) string {
	creds := username + ":" + password
	return "Basic " + base64.StdEncoding.EncodeToString([]byte(creds))
}

// TestBasicAuthMiddleware_ValidCredentials verifies that a request carrying the
// correct username and password is forwarded to the next handler and receives 200.
func TestBasicAuthMiddleware_ValidCredentials(t *testing.T) {
	const (
		username = "admin"
		password = "s3cr3t"
	)

	router := newBasicAuthRouter(username, hashPassword(t, password))

	req := httptest.NewRequest(http.MethodGet, "/test", nil)
	req.Header.Set("Authorization", basicAuthHeader(username, password))
	rr := httptest.NewRecorder()

	router.ServeHTTP(rr, req)

	require.Equal(t, http.StatusOK, rr.Code)
	require.Equal(t, "ok", rr.Body.String())
}

// TestBasicAuthMiddleware_WrongPassword verifies that a request with the correct
// username but wrong password receives 401 with a WWW-Authenticate header.
func TestBasicAuthMiddleware_WrongPassword(t *testing.T) {
	const username = "admin"
	router := newBasicAuthRouter(username, hashPassword(t, "correct-password"))

	req := httptest.NewRequest(http.MethodGet, "/test", nil)
	req.Header.Set("Authorization", basicAuthHeader(username, "wrong-password"))
	rr := httptest.NewRecorder()

	router.ServeHTTP(rr, req)

	require.Equal(t, http.StatusUnauthorized, rr.Code)
	require.Equal(t, `Basic realm="BloodHound"`, rr.Header().Get("WWW-Authenticate"))
}

// TestBasicAuthMiddleware_WrongUsername verifies that a request with the wrong
// username (even with a correct password hash for a different user) receives 401.
func TestBasicAuthMiddleware_WrongUsername(t *testing.T) {
	const password = "s3cr3t"
	router := newBasicAuthRouter("admin", hashPassword(t, password))

	req := httptest.NewRequest(http.MethodGet, "/test", nil)
	req.Header.Set("Authorization", basicAuthHeader("notadmin", password))
	rr := httptest.NewRecorder()

	router.ServeHTTP(rr, req)

	require.Equal(t, http.StatusUnauthorized, rr.Code)
	require.Equal(t, `Basic realm="BloodHound"`, rr.Header().Get("WWW-Authenticate"))
}

// TestBasicAuthMiddleware_MissingAuthorizationHeader verifies that a request with
// no Authorization header receives 401.
func TestBasicAuthMiddleware_MissingAuthorizationHeader(t *testing.T) {
	router := newBasicAuthRouter("admin", hashPassword(t, "s3cr3t"))

	req := httptest.NewRequest(http.MethodGet, "/test", nil)
	rr := httptest.NewRecorder()

	router.ServeHTTP(rr, req)

	require.Equal(t, http.StatusUnauthorized, rr.Code)
	require.Equal(t, `Basic realm="BloodHound"`, rr.Header().Get("WWW-Authenticate"))
}

// TestBasicAuthMiddleware_WrongScheme verifies that a request using a scheme
// other than "Basic" (e.g. "Bearer") receives 401.
func TestBasicAuthMiddleware_WrongScheme(t *testing.T) {
	router := newBasicAuthRouter("admin", hashPassword(t, "s3cr3t"))

	req := httptest.NewRequest(http.MethodGet, "/test", nil)
	req.Header.Set("Authorization", "Bearer sometoken")
	rr := httptest.NewRecorder()

	router.ServeHTTP(rr, req)

	require.Equal(t, http.StatusUnauthorized, rr.Code)
	require.Equal(t, `Basic realm="BloodHound"`, rr.Header().Get("WWW-Authenticate"))
}

// TestBasicAuthMiddleware_TruncatedBase64 verifies that a malformed Basic header
// whose base64 payload is truncated (not valid base64) receives 401.
func TestBasicAuthMiddleware_TruncatedBase64(t *testing.T) {
	router := newBasicAuthRouter("admin", hashPassword(t, "s3cr3t"))

	req := httptest.NewRequest(http.MethodGet, "/test", nil)
	// "Basic " followed by clearly broken base64
	req.Header.Set("Authorization", "Basic !!!notbase64!!!")
	rr := httptest.NewRecorder()

	router.ServeHTTP(rr, req)

	require.Equal(t, http.StatusUnauthorized, rr.Code)
	require.Equal(t, `Basic realm="BloodHound"`, rr.Header().Get("WWW-Authenticate"))
}

// TestBasicAuthMiddleware_MissingColon verifies that a Base64 payload that decodes
// successfully but contains no colon separator receives 401.
func TestBasicAuthMiddleware_MissingColon(t *testing.T) {
	router := newBasicAuthRouter("admin", hashPassword(t, "s3cr3t"))

	// base64("nocolon") — no ":" separator between username and password
	encoded := base64.StdEncoding.EncodeToString([]byte("nocolon"))
	req := httptest.NewRequest(http.MethodGet, "/test", nil)
	req.Header.Set("Authorization", "Basic "+encoded)
	rr := httptest.NewRecorder()

	router.ServeHTTP(rr, req)

	require.Equal(t, http.StatusUnauthorized, rr.Code)
	require.Equal(t, `Basic realm="BloodHound"`, rr.Header().Get("WWW-Authenticate"))
}

// TestBasicAuthMiddleware_EmptyUsernameConfigured verifies the behaviour when the
// middleware is configured with an empty username. A request must supply a
// matching (empty) username; providing any non-empty username is rejected.
func TestBasicAuthMiddleware_EmptyUsernameConfigured(t *testing.T) {
	const password = "s3cr3t"
	// Configure middleware with an empty username string.
	router := newBasicAuthRouter("", hashPassword(t, password))

	// A request with a non-empty username should be rejected.
	req := httptest.NewRequest(http.MethodGet, "/test", nil)
	req.Header.Set("Authorization", basicAuthHeader("someuser", password))
	rr := httptest.NewRecorder()

	router.ServeHTTP(rr, req)

	require.Equal(t, http.StatusUnauthorized, rr.Code)
}

// TestBasicAuthMiddleware_EmptyUsernameAndPasswordConfigured verifies that when
// both username and password hash are empty/nil the middleware rejects all
// requests (there is no valid credential to present).
func TestBasicAuthMiddleware_EmptyUsernameAndPasswordConfigured(t *testing.T) {
	// Passing a nil/empty hash means bcrypt.CompareHashAndPassword will always
	// return an error, so every request is rejected.
	router := newBasicAuthRouter("", nil)

	req := httptest.NewRequest(http.MethodGet, "/test", nil)
	req.Header.Set("Authorization", basicAuthHeader("", ""))
	rr := httptest.NewRecorder()

	router.ServeHTTP(rr, req)

	require.Equal(t, http.StatusUnauthorized, rr.Code)
}

// TestBasicAuthMiddleware_WWWAuthenticateHeaderOnMissingAuth ensures the
// WWW-Authenticate header is always present on 401 responses, including when
// no Authorization header was sent at all.
func TestBasicAuthMiddleware_WWWAuthenticateHeaderOnMissingAuth(t *testing.T) {
	router := newBasicAuthRouter("user", hashPassword(t, "pass"))

	req := httptest.NewRequest(http.MethodGet, "/test", nil)
	rr := httptest.NewRecorder()

	router.ServeHTTP(rr, req)

	require.Equal(t, http.StatusUnauthorized, rr.Code)
	wwwAuth := rr.Header().Get("WWW-Authenticate")
	require.NotEmpty(t, wwwAuth, "WWW-Authenticate header must be present on 401")
	require.Equal(t, `Basic realm="BloodHound"`, wwwAuth)
}

// TestBasicAuthMiddleware_HandlerNotCalledOnFailure verifies that the downstream
// handler is NOT invoked when authentication fails.
func TestBasicAuthMiddleware_HandlerNotCalledOnFailure(t *testing.T) {
	handlerCalled := false

	r := mux.NewRouter()
	r.Use(middleware.BasicAuthMiddleware("admin", hashPassword(t, "secret")))
	r.HandleFunc("/test", func(w http.ResponseWriter, req *http.Request) {
		handlerCalled = true
		w.WriteHeader(http.StatusOK)
	}).Methods(http.MethodGet)

	req := httptest.NewRequest(http.MethodGet, "/test", nil)
	req.Header.Set("Authorization", basicAuthHeader("admin", "wrongpassword"))
	rr := httptest.NewRecorder()

	r.ServeHTTP(rr, req)

	require.False(t, handlerCalled, "downstream handler must not be called when auth fails")
	require.Equal(t, http.StatusUnauthorized, rr.Code)
}
