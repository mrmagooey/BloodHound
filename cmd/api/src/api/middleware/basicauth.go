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

package middleware

import (
	"net/http"

	"github.com/gorilla/mux"
	"golang.org/x/crypto/bcrypt"
)

// BasicAuthMiddleware returns a mux.MiddlewareFunc that enforces HTTP Basic Auth using a pre-hashed
// bcrypt password. Every request must supply a matching Authorization: Basic header or receive a
// 401 Unauthorized response with a WWW-Authenticate challenge.
//
// hashedPassword must be a bcrypt hash produced by bcrypt.GenerateFromPassword.
// username is compared as-is (case-sensitive).
func BasicAuthMiddleware(username string, hashedPassword []byte) mux.MiddlewareFunc {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			gotUser, gotPass, ok := r.BasicAuth()
			if !ok || gotUser != username || bcrypt.CompareHashAndPassword(hashedPassword, []byte(gotPass)) != nil {
				w.Header().Set("WWW-Authenticate", `Basic realm="BloodHound"`)
				http.Error(w, "Unauthorized", http.StatusUnauthorized)
				return
			}
			next.ServeHTTP(w, r)
		})
	}
}
