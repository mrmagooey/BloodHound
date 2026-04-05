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
	"context"
	"log/slog"
	"net/http"
	"sync"

	"github.com/gorilla/mux"
	"github.com/specterops/bloodhound/cmd/api/src/auth"
	bhCtx "github.com/specterops/bloodhound/cmd/api/src/ctx"
	"github.com/specterops/bloodhound/cmd/api/src/model"
)

// UserLookup is a minimal interface for looking up a user by principal name.
type UserLookup interface {
	LookupUser(ctx context.Context, principalName string) (model.User, error)
}

// StandaloneAuthMiddleware replaces AuthMiddleware in standalone (SQLite) mode.
// It unconditionally authenticates every request as the default admin user,
// ignoring any Authorization header. This is appropriate because standalone mode
// is a single-user, local-only deployment without real authentication.
//
// The admin user is cached after the first successful lookup to avoid hitting
// the database on every request, which is important when the SQLite connection
// pool is limited to a single connection.
func StandaloneAuthMiddleware(db UserLookup, principalName string) mux.MiddlewareFunc {
	var (
		cachedUser model.User
		cached     bool
		mu         sync.Mutex
	)

	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
			bCtx := bhCtx.Get(request.Context())

			mu.Lock()
			if cached {
				bCtx.AuthCtx = auth.Context{Owner: cachedUser}
				mu.Unlock()
			} else {
				mu.Unlock()
				// Use a background context so the DB lookup is not cancelled
				// if the HTTP request is aborted before the query completes.
				if adminUser, err := db.LookupUser(context.Background(), principalName); err != nil {
					slog.ErrorContext(request.Context(), "StandaloneAuthMiddleware: failed to look up admin user", "error", err)
				} else {
					mu.Lock()
					cachedUser = adminUser
					cached = true
					mu.Unlock()
					bCtx.AuthCtx = auth.Context{Owner: adminUser}
				}
			}

			next.ServeHTTP(response, request)
		})
	}
}
