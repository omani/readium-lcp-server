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

package lsdserver

import (
	"bytes"
	"encoding/json"
	"errors"
	"strings"
	"time"

	"context"
	"io/ioutil"

	"github.com/jinzhu/gorm"
	"github.com/readium/readium-lcp-server/lib/http"
	"github.com/readium/readium-lcp-server/lib/i18n"
	"github.com/readium/readium-lcp-server/model"
)

// getEvents gets the events from database for the license status
//
func getEvents(ls *model.LicenseStatus, s http.IServer) error {
	var err error
	ls.Events, err = s.Store().Transaction().GetByLicenseStatusId(ls.Id)
	if err != gorm.ErrRecordNotFound {
		return err
	}
	return nil
}

// makeLinks creates and adds links to the license status
//
func makeLinks(ls *model.LicenseStatus, lsdConfig http.LsdServerInfo, lcpConfig http.ServerInfo, licStatus http.LicenseStatus) {
	lsdBaseURL := lsdConfig.PublicBaseUrl
	licenseLinkURL := lsdConfig.LicenseLinkUrl
	lcpBaseURL := lcpConfig.PublicBaseUrl
	//frontendBaseUrl := config.Config.FrontendServer.PublicBaseUrl
	registerAvailable := licStatus.Register

	licenseHasRightsEnd := ls.CurrentEndLicense.Valid && !(ls.CurrentEndLicense.Time).IsZero()
	returnAvailable := licStatus.Return && licenseHasRightsEnd
	renewAvailable := licStatus.Renew && licenseHasRightsEnd

	links := make(model.LicenseLinksCollection, 0, 0)

	if licenseLinkURL != "" {
		licenseLinkURLReal := strings.Replace(licenseLinkURL, "{license_id}", ls.LicenseRef, -1)
		link := &model.LicenseLink{
			Href:      licenseLinkURLReal,
			Rel:       "license",
			Type:      http.ContentTypeLcpJson,
			Templated: false,
		}
		links = append(links, link)
	} else {
		link := &model.LicenseLink{
			Href:      lcpBaseURL + "/licenses/" + ls.LicenseRef,
			Rel:       "license",
			Type:      http.ContentTypeLcpJson,
			Templated: false,
		}
		links = append(links, link)
	}

	if registerAvailable {
		link := &model.LicenseLink{
			Href:      lsdBaseURL + "/licenses/" + ls.LicenseRef + "/register{?id,name}",
			Rel:       "register",
			Type:      http.ContentTypeLsdJson,
			Templated: true,
		}
		links = append(links, link)
	}

	if returnAvailable {
		link := &model.LicenseLink{
			Href:      lsdBaseURL + "/licenses/" + ls.LicenseRef + "/return{?id,name}",
			Rel:       "return",
			Type:      http.ContentTypeLsdJson,
			Templated: true,
		}
		links = append(links, link)
	}

	if renewAvailable {
		link := &model.LicenseLink{
			Href:      lsdBaseURL + "/licenses/" + ls.LicenseRef + "/renew{?end,id,name}",
			Rel:       "renew",
			Type:      http.ContentTypeLsdJson,
			Templated: true,
		}
		links = append(links, link)
	}

	ls.Links = links
}

// makeEvent creates an event and fill it
//
func makeEvent(status model.Status, deviceName string, deviceID string, licenseStatusFk int64) *model.TransactionEvent {
	return &model.TransactionEvent{
		DeviceId:        deviceID,
		DeviceName:      deviceName,
		Timestamp:       time.Now().UTC().Truncate(time.Second),
		Type:            status,
		LicenseStatusFk: licenseStatusFk,
	}
}

// notifyLCPServer updates a license by calling the License Server
// called from return, renew and cancel/revoke actions
//
func notifyLCPServer(timeEnd time.Time, licenseID string, s http.IServer) (int, error) {
	lcpConfig, updateAuth := s.Config().LcpServer, s.Config().LcpUpdateAuth
	// get the lcp server url
	lcpBaseURL := lcpConfig.PublicBaseUrl
	if len(lcpBaseURL) <= 0 {
		return 0, errors.New("Undefined Config.LcpServer.PublicBaseUrl")
	}
	// create a minimum license object, limited to the license id plus rights
	// FIXME: remove the id (here and in the lcpserver license.go)
	minLicense := model.License{Id: licenseID, Rights: &model.LicenseUserRights{}}
	// set the new end date
	minLicense.Rights.End = &model.NullTime{Valid: true, Time: timeEnd}

	// prepare the request
	lcpURL := lcpBaseURL + "/licenses/" + licenseID
	// message to the console
	s.LogInfo("PATCH " + lcpURL)
	payload, err := json.Marshal(minLicense)
	// send the content to the LCP server
	req, err := http.NewRequest("PATCH", lcpURL, bytes.NewReader(payload))
	if err != nil {
		return 0, err
	}
	// set the credentials
	if updateAuth.Username != "" {
		req.SetBasicAuth(updateAuth.Username, updateAuth.Password)
	}
	// set the content type
	req.Header.Add(http.HdrContentType, http.ContentTypeLcpJson)

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
		s.LogError("Error Notify Lcp Server of License (%q): %v", licenseID, err)
		return 0, err
	}

	// we have a body, defering close
	defer resp.Body.Close()
	// reading body
	body, err := ioutil.ReadAll(resp.Body)
	if err != nil {
		s.LogError("Notify LsdServer of compliancetest reading body error : %v", err)
		return 0, err
	}
	if resp.StatusCode != http.StatusOK {
		s.LogError("Error Notify Lcp Server of License (%q) response %v [http-status:%d]", licenseID, body, resp.StatusCode)
	}
	return resp.StatusCode, nil
}

// fillLicenseStatus fills the localized 'message' field, the 'links' and 'event' objects in the license status
//
func fillLicenseStatus(ls *model.LicenseStatus, r *http.Request, s http.IServer) error {
	// add the localized message
	acceptLanguages := r.Header.Get("Accept-Language")
	license := ""
	i18n.LocalizeMessage(s.Config().Localization.DefaultLanguage, acceptLanguages, &license, ls.Status.String())
	// add the links
	makeLinks(ls, s.Config().LsdServer, s.Config().LcpServer, s.Config().LicenseStatus)
	// add the events
	err := getEvents(ls, s)

	return err
}
