// Copyright (c) 2016 Readium Foundation
//
// Redistribution and use in source and binary forms, with or without modification,
// are permitted provided that the following conditions are met:
//
// 1. Redistributions of source code must retain the above copyright notice, this
//    list of conditions and the following disclaimer.
// 2. Redistributions in binary form must reproduce the above copyright notice,
//    this list of conditions and the following disclaimer in the documentation and/or
//    other materials provided with the distribution.
// 3. Neither the name of the organization nor the names of its contributors may be
//    used to endorse or promote products derived from this software without specific
//    prior written permission
//
// THIS SOFTWARE IS PROVIDED BY THE COPYRIGHT HOLDERS AND CONTRIBUTORS "AS IS" AND
// ANY EXPRESS OR IMPLIED WARRANTIES, INCLUDING, BUT NOT LIMITED TO, THE IMPLIED
// WARRANTIES OF MERCHANTABILITY AND FITNESS FOR A PARTICULAR PURPOSE ARE
// DISCLAIMED. IN NO EVENT SHALL THE COPYRIGHT OWNER OR CONTRIBUTORS BE LIABLE FOR
// ANY DIRECT, INDIRECT, INCIDENTAL, SPECIAL, EXEMPLARY, OR CONSEQUENTIAL DAMAGES
// (INCLUDING, BUT NOT LIMITED TO, PROCUREMENT OF SUBSTITUTE GOODS OR SERVICES;
// LOSS OF USE, DATA, OR PROFITS; OR BUSINESS INTERRUPTION) HOWEVER CAUSED AND
// ON ANY THEORY OF LIABILITY, WHETHER IN CONTRACT, STRICT LIABILITY, OR TORT
// (INCLUDING NEGLIGENCE OR OTHERWISE) ARISING IN ANY WAY OUT OF THE USE OF THIS
// SOFTWARE, EVEN IF ADVISED OF THE POSSIBILITY OF SUCH DAMAGE. 

package api

import (
	"net/http"
	"github.com/gorilla/mux"
	"github.com/urfave/negroni"
	"github.com/abbot/go-http-auth"
	
	"github.com/technoweenie/grohl"

	"github.com/readium/readium-lcp-server/problem"
)

const (
	ContentType_LCP_JSON = "application/vnd.readium.lcp.license.1.0+json"
	ContentType_LSD_JSON = "application/vnd.readium.license.status.v1.0+json"
	
	ContentType_JSON = "application/json"

	ContentType_FORM_URL_ENCODED = "application/x-www-form-urlencoded"
)

type ServerRouter struct {
	R *mux.Router
	N *negroni.Negroni	
}

func CreateServerRouter(tplPath string) (ServerRouter) {
	 
	r := mux.NewRouter()

	r.NotFoundHandler = http.HandlerFunc(problem.NotFoundHandler) //handle all other requests 404

	// this demonstrates a panic report
	r.HandleFunc("/panic", func(w http.ResponseWriter, req *http.Request) {
		panic("just testing. no worries.")
	})

	//n := negroni.Classic() == negroni.New(negroni.NewRecovery(), negroni.NewLogger(), negroni.NewStatic(...))
	n := negroni.New()

	// possibly useful middlewares:
	// https://github.com/jeffbmartinez/delay

	//https://github.com/urfave/negroni#recovery
	recovery := negroni.NewRecovery()
	recovery.PrintStack = true
	recovery.ErrorHandlerFunc = problem.PanicReport
	n.Use(recovery)

	//https://github.com/urfave/negroni#logger
	n.Use(negroni.NewLogger())

	n.Use(negroni.HandlerFunc(ExtraLogger))
	
	if tplPath != "" {
		//https://github.com/urfave/negroni#static
		n.Use(negroni.NewStatic(http.Dir(tplPath)))
	}

	n.Use(negroni.HandlerFunc(CORSHeaders))
	// Does not insert CORS headers as intended, depends on Origin check in the HTTP request...we want the same headers, always.
	// IMPORT "github.com/rs/cors"
	// //https://github.com/rs/cors#parameters
	// c := cors.New(cors.Options{
	// 	AllowedOrigins: []string{"*"},
	// 	AllowedMethods: []string{"POST", "GET", "OPTIONS", "PUT", "DELETE"},
	// 	Debug: true,
	// })
	// n.Use(c)
	
	n.UseHandler(r)

	sr := ServerRouter{
		R: r,
		N: n,
	}
	
	return sr
}

func ExtraLogger(rw http.ResponseWriter, r *http.Request, next http.HandlerFunc) {

	grohl.Log(grohl.Data{"method": r.Method, "path": r.URL.Path})

// before	
	next(rw, r)
// after

	// noop
}

func CORSHeaders(rw http.ResponseWriter, r *http.Request, next http.HandlerFunc) {

	grohl.Log(grohl.Data{"CORS": "yes"})
	rw.Header().Add("Access-Control-Allow-Methods", "POST, GET, OPTIONS, PUT, DELETE")
	rw.Header().Add("Access-Control-Allow-Origin", "*")

// before	
	next(rw, r)
// after

	// noop
}

func CheckAuth(authenticator *auth.BasicAuth, w http.ResponseWriter, r *http.Request) (bool) {
	var username string
	if username = authenticator.CheckAuth(r); username == "" {
		grohl.Log(grohl.Data{"error": "Unauthorized", "method": r.Method, "path": r.URL.Path})
		w.Header().Set("WWW-Authenticate", `Basic realm="`+authenticator.Realm+`"`)
		problem.Error(w, r, problem.Problem{Type: "about:blank", Detail: "User or password do not match!"}, http.StatusUnauthorized)
		return false
	}
	grohl.Log(grohl.Data{"user": username})
	return true
}
