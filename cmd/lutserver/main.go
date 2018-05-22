/*
 * Copyright (c) 2016-2018 Readium Foundation
 *
 * Redistribution and use in source and binary forms, with or without modification,
 * are permitted provided that the following conditions are met:
 *
 *  1. Redistributions of source code must retain the above copyright notice, this
 *     list of conditions and the following disclaimer.
 *  2. Redistributions in binary form must reproduce the above copyright notice,
 *     this list of conditions and the following disclaimer in the documentation and/or
 *     other materials provided with the distribution.
 *  3. Neither the name of the organization nor the names of its contributors may be
 *     used to endorse or promote products derived from this software without specific
 *     prior written permission
 *
 *  THIS SOFTWARE IS PROVIDED BY THE COPYRIGHT HOLDERS AND CONTRIBUTORS "AS IS" AND
 *  ANY EXPRESS OR IMPLIED WARRANTIES, INCLUDING, BUT NOT LIMITED TO, THE IMPLIED
 *  WARRANTIES OF MERCHANTABILITY AND FITNESS FOR A PARTICULAR PURPOSE ARE
 *  DISCLAIMED. IN NO EVENT SHALL THE COPYRIGHT OWNER OR CONTRIBUTORS BE LIABLE FOR
 *  ANY DIRECT, INDIRECT, INCIDENTAL, SPECIAL, EXEMPLARY, OR CONSEQUENTIAL DAMAGES
 *  (INCLUDING, BUT NOT LIMITED TO, PROCUREMENT OF SUBSTITUTE GOODS OR SERVICES;
 *  LOSS OF USE, DATA, OR PROFITS; OR BUSINESS INTERRUPTION) HOWEVER CAUSED AND
 *  ON ANY THEORY OF LIABILITY, WHETHER IN CONTRACT, STRICT LIABILITY, OR TORT
 *  (INCLUDING NEGLIGENCE OR OTHERWISE) ARISING IN ANY WAY OUT OF THE USE OF THIS
 *  SOFTWARE, EVEN IF ADVISED OF THE POSSIBILITY OF SUCH DAMAGE.
 */

package main

import (
	goHttp "net/http"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"time"

	"github.com/readium/readium-lcp-server/lib/http"
	"github.com/readium/readium-lcp-server/lib/logger"
	"github.com/readium/readium-lcp-server/model"

	"context"
	"encoding/base64"
	"encoding/json"
	"io/ioutil"
	"os/signal"

	"github.com/gorilla/mux"
	"github.com/readium/readium-lcp-server/controller/lutserver"
	"github.com/readium/readium-lcp-server/lib/cron"
)

func main() {
	// Start logger
	log := logger.New()
	log.Printf("RUNNING UTIL SERVER")

	var dbURI, configFile string
	var err error

	if configFile = os.Getenv("READIUM_FRONTEND_CONFIG"); configFile == "" {
		configFile = "config.yaml"
	}
	cfg, err := http.ReadConfig(configFile)
	if err != nil {
		panic(err)
	}

	log.Printf("LCP server = %s", cfg.LcpServer.PublicBaseUrl)
	log.Printf("using login  %s ", cfg.LcpUpdateAuth.Username)
	// use a sqlite db by default
	if dbURI = cfg.FrontendServer.Database; dbURI == "" {
		dbURI = "sqlite3://file:frontend.sqlite?cache=shared&mode=rwc"
	}

	stor, err := model.SetupDB(dbURI, log, false)
	if err != nil {
		panic("Error setting up the database : " + err.Error())
	}
	err = stor.AutomigrateForFrontend()
	if err != nil {
		panic("Error migrating database : " + err.Error())
	}

	server := New(cfg, log, stor)
	log.Printf("Frontend webserver for LCP running on " + cfg.FrontendServer.Host + ":" + strconv.Itoa(cfg.FrontendServer.Port))

	// Run our server in a goroutine so that it doesn't block.
	go func() {
		if err := server.ListenAndServe(); err != nil {
			log.Printf("Error " + err.Error())
		}
	}()

	c := make(chan os.Signal, 1)
	// We'll accept graceful shutdowns when quit via SIGINT (Ctrl+C)
	// SIGKILL, SIGQUIT or SIGTERM (Ctrl+/) will not be caught.
	signal.Notify(c, os.Interrupt)

	// Block until we receive our signal.
	<-c

	wait := time.Second * 15 // the duration for which the server gracefully wait for existing connections to finish
	// Create a deadline to wait for.
	ctx, cancel := context.WithTimeout(context.Background(), wait)
	defer cancel()
	// Doesn't block if no connections, but will otherwise wait
	// until the timeout deadline.
	server.Shutdown(ctx)
	// Optionally, you could run srv.Shutdown in a goroutine and block on
	// <-ctx.Done() if your application should wait for other services
	// to finalize based on context cancellation.
	log.Printf("server is shutting down.")
	os.Exit(0)
}

// New creates a new webserver (basic user interface)
func New(
	cfg http.Configuration,
	log logger.StdLogger,
	store model.Store) *http.Server {

	tcpAddress := cfg.FrontendServer.Host + ":" + strconv.Itoa(cfg.FrontendServer.Port)

	static := cfg.FrontendServer.Directory
	if static == "" {
		_, file, _, _ := runtime.Caller(0)
		here := filepath.Dir(file)
		static = filepath.Join(here, "../frontend/manage")
	}

	filepathConfigJs := filepath.Join(static, "config.js")
	fileConfigJs, err := os.Create(filepathConfigJs)
	if err != nil {
		panic(err)
	}

	defer func() {
		if err := fileConfigJs.Close(); err != nil {
			panic(err)
		}
	}()

	configJs := `
	// This file is automatically generated, and git-ignored.
	// To ignore your local changes, use:
	// git update-index --assume-unchanged frontend/manage/config.js
	window.Config = {`
	configJs += "\n\tfrontend: {url: '" + cfg.FrontendServer.PublicBaseUrl + "' },\n"
	configJs += "\tlcp: {url: '" + cfg.LcpServer.PublicBaseUrl + "', user: '" + cfg.LcpUpdateAuth.Username + "', password: '" + cfg.LcpUpdateAuth.Password + "'},\n"
	configJs += "\tlsd: {url: '" + cfg.LsdServer.PublicBaseUrl + "', user: '" + cfg.LsdNotifyAuth.Username + "', password: '" + cfg.LsdNotifyAuth.Password + "'}\n}"

	log.Printf("manage/index.html config.js:")
	log.Printf(configJs)

	fileConfigJs.WriteString(configJs)
	log.Printf("... written in %s", filepathConfigJs)

	log.Printf("Static folder : %s", static)
	muxer := mux.NewRouter()

	muxer.Use(
		http.RecoveryHandler(http.RecoveryLogger(log), http.PrintRecoveryStack(true)),
		http.CorsMiddleWare(
			http.AllowedOrigins([]string{"*"}),
			http.AllowedMethods([]string{"PATCH", "HEAD", "POST", "GET", "OPTIONS", "PUT", "DELETE"}),
			http.AllowedHeaders([]string{"Range", "Content-Type", "Origin", "X-Requested-With", "Accept", "Accept-Language", "Content-Language", "Authorization"}),
		),
		http.DelayMiddleware,
	)

	if static != "" {
		// TODO : fix this.
		muxer.PathPrefix("/static/").Handler(goHttp.StripPrefix("/static/", goHttp.FileServer(goHttp.Dir(static))))
	}

	server := &http.Server{
		Server: goHttp.Server{
			Handler:        muxer,
			Addr:           tcpAddress,
			WriteTimeout:   15 * time.Second,
			ReadTimeout:    15 * time.Second,
			MaxHeaderBytes: 1 << 20,
		},
		Log:   log,
		Cfg:   cfg,
		Model: store,
	}

	muxer.NotFoundHandler = server.NotFoundHandler() //handle all other requests 404
	// Cron, get license status information
	cron.Start(5 * time.Minute)
	// using Method expression instead of function
	cron.Every(1).Minutes().Do(func() {
		println("FetchLicenseStatusesFromLSD")
		FetchLicenseStatusesFromLSD(server)
	})

	apiURLPrefix := "/api/v1"
	//
	//  repositories of master files
	//
	repositoriesRoutesPathPrefix := apiURLPrefix + "/repositories"
	repositoriesRoutes := muxer.PathPrefix(repositoriesRoutesPathPrefix).Subrouter().StrictSlash(false)
	//
	server.HandleFunc(repositoriesRoutes, "/master-files", lutserver.GetRepositoryMasterFiles).Methods("GET")
	//
	// dashboard
	//
	server.HandleFunc(muxer, "/dashboardInfos", lutserver.GetDashboardInfos).Methods("GET")
	server.HandleFunc(muxer, "/dashboardBestSellers", lutserver.GetDashboardBestSellers).Methods("GET")
	//
	// publications
	//
	publicationsRoutesPathPrefix := apiURLPrefix + "/publications"
	publicationsRoutes := muxer.PathPrefix(publicationsRoutesPathPrefix).Subrouter().StrictSlash(false)
	//
	server.HandleFunc(muxer, publicationsRoutesPathPrefix, lutserver.GetPublications).Methods("GET")
	//
	server.HandleFunc(muxer, publicationsRoutesPathPrefix, lutserver.CreatePublication).Methods("POST")
	//
	server.HandleFunc(muxer, "/PublicationUpload", lutserver.UploadEPUB).Methods("POST")
	//
	server.HandleFunc(publicationsRoutes, "/check-by-title", lutserver.CheckPublicationByTitle).Methods("GET")
	//
	server.HandleFunc(publicationsRoutes, "/{id}", lutserver.GetPublication).Methods("GET")
	server.HandleFunc(publicationsRoutes, "/{id}", lutserver.UpdatePublication).Methods("PUT")
	server.HandleFunc(publicationsRoutes, "/{id}", lutserver.DeletePublication).Methods("DELETE")
	//
	// user functions
	//
	usersRoutesPathPrefix := apiURLPrefix + "/users"
	usersRoutes := muxer.PathPrefix(usersRoutesPathPrefix).Subrouter().StrictSlash(false)
	//
	server.HandleFunc(muxer, usersRoutesPathPrefix, lutserver.GetUsers).Methods("GET")
	//
	server.HandleFunc(muxer, usersRoutesPathPrefix, lutserver.CreateUser).Methods("POST")
	//
	server.HandleFunc(usersRoutes, "/{id}", lutserver.GetUser).Methods("GET")
	server.HandleFunc(usersRoutes, "/{id}", lutserver.UpdateUser).Methods("PUT")
	server.HandleFunc(usersRoutes, "/{id}", lutserver.DeleteUser).Methods("DELETE")
	// get all purchases for a given user
	server.HandleFunc(usersRoutes, "/{user_id}/purchases", lutserver.GetUserPurchases).Methods("GET")

	//
	// purchases
	//
	purchasesRoutesPathPrefix := apiURLPrefix + "/purchases"
	purchasesRoutes := muxer.PathPrefix(purchasesRoutesPathPrefix).Subrouter().StrictSlash(false)
	// get all purchases
	server.HandleFunc(muxer, purchasesRoutesPathPrefix, lutserver.GetPurchases).Methods("GET")
	// create a purchase
	server.HandleFunc(muxer, purchasesRoutesPathPrefix, lutserver.CreatePurchase).Methods("POST")
	// update a purchase
	server.HandleFunc(purchasesRoutes, "/{id}", lutserver.UpdatePurchase).Methods("PUT")
	// get a purchase by purchase id
	server.HandleFunc(purchasesRoutes, "/{id}", lutserver.GetPurchase).Methods("GET")
	// get a license from the associated purchase id
	server.HandleFunc(purchasesRoutes, "/{id}/license", lutserver.GetPurchasedLicense).Methods("GET")
	//
	// licences
	//
	licenseRoutesPathPrefix := apiURLPrefix + "/licenses"
	licenseRoutes := muxer.PathPrefix(licenseRoutesPathPrefix).Subrouter().StrictSlash(false)
	//
	// get a list of licenses
	server.HandleFunc(muxer, licenseRoutesPathPrefix, lutserver.GetFilteredLicenses).Methods("GET")
	// get a license by id
	server.HandleFunc(licenseRoutes, "/{license_id}", lutserver.GetLicense).Methods("GET")

	return server
}

func ReadLicensesPayloads(data []byte) (model.LicensesStatusCollection, error) {
	var licenses model.LicensesStatusCollection
	err := json.Unmarshal(data, &licenses)
	if err != nil {
		return nil, err
	}
	return licenses, nil
}

// TODO : move this outof here
func FetchLicenseStatusesFromLSD(s http.IServer) {
	s.LogInfo("AUTOMATION : Fetch and save all license status documents")

	url := s.Config().LsdServer.PublicBaseUrl + "/licenses"
	auth := s.Config().LsdNotifyAuth

	// prepare the request
	req, err := http.NewRequest("GET", url, nil)
	if err != nil {
		panic(err)
	}
	req.Header.Set("Authorization", "Basic "+base64.StdEncoding.EncodeToString([]byte(auth.Username+":"+auth.Password)))
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	// making request
	resp, err := http.DefaultClient.Do(req.WithContext(ctx))
	// If we got an error, and the context has been canceled, the context's error is probably more useful.
	if err != nil {
		select {
		case <-ctx.Done():
			err = ctx.Err()
		default:
		}
	}

	if err != nil {
		s.LogError("AUTOMATION : Error getting license statuses : %v", err)
		return
	}

	// we have a body, defering close
	defer resp.Body.Close()
	// reading body
	body, err := ioutil.ReadAll(resp.Body)
	if err != nil {
		s.LogError("AUTOMATION : Error reading response body error : %v", err)
	}

	s.LogInfo("AUTOMATION : lsd server response : %v [http-status:%d]", body, resp.StatusCode)

	// clear the db
	err = s.Store().License().PurgeDataBase()
	if err != nil {
		panic(err)
	}

	licenses, err := ReadLicensesPayloads(body)
	if err != nil {
		panic(err)
	}
	// fill the db
	err = s.Store().License().BulkAdd(licenses)
	if err != nil {
		panic(err)
	}
}
