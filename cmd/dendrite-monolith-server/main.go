// Copyright 2017 Vector Creations Ltd
//
// Licensed under the Apache License, Version 2.0 (the "License");
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

package main

import (
	"flag"
	"net/http"

	"github.com/matrix-org/dendrite/appservice"
	"github.com/matrix-org/dendrite/clientapi"
	"github.com/matrix-org/dendrite/clientapi/producers"
	"github.com/matrix-org/dendrite/eduserver"
	"github.com/matrix-org/dendrite/eduserver/cache"
	"github.com/matrix-org/dendrite/federationapi"
	"github.com/matrix-org/dendrite/federationsender"
	"github.com/matrix-org/dendrite/internal"
	"github.com/matrix-org/dendrite/internal/config"
	"github.com/matrix-org/dendrite/internal/setup"
	"github.com/matrix-org/dendrite/internal/transactions"
	"github.com/matrix-org/dendrite/keyserver"
	"github.com/matrix-org/dendrite/mediaapi"
	"github.com/matrix-org/dendrite/publicroomsapi"
	"github.com/matrix-org/dendrite/roomserver"
	"github.com/matrix-org/dendrite/serverkeyapi"
	"github.com/matrix-org/dendrite/syncapi"

	"github.com/sirupsen/logrus"
)

var (
	httpBindAddr   = flag.String("http-bind-address", ":8008", "The HTTP listening port for the server")
	httpsBindAddr  = flag.String("https-bind-address", ":8448", "The HTTPS listening port for the server")
	certFile       = flag.String("tls-cert", "", "The PEM formatted X509 certificate to use for TLS")
	keyFile        = flag.String("tls-key", "", "The PEM private key to use for TLS")
	enableHTTPAPIs = flag.Bool("api", false, "Use HTTP APIs instead of short-circuiting (warning: exposes API endpoints!)")
)

func main() {
	cfg := setup.ParseFlags(true)
	if *enableHTTPAPIs {
		// If the HTTP APIs are enabled then we need to update the Listen
		// statements in the configuration so that we know where to find
		// the API endpoints. They'll listen on the same port as the monolith
		// itself.
		addr := config.Address(*httpBindAddr)
		cfg.Listen.RoomServer = addr
		cfg.Listen.EDUServer = addr
		cfg.Listen.AppServiceAPI = addr
		cfg.Listen.FederationSender = addr
		cfg.Listen.ServerKeyAPI = addr
	}

	base := setup.NewBase(cfg, "Monolith", *enableHTTPAPIs)
	defer base.Close() // nolint: errcheck

	serverKeyAPI := serverkeyapi.SetupServerKeyAPIComponent(base)
	if !base.UseHTTPAPIs {
		base.SetServerKeyAPI(serverKeyAPI)
	}

	rsAPI := roomserver.SetupRoomServerComponent(base)
	if !base.UseHTTPAPIs {
		base.SetRoomserverAPI(rsAPI)
	}

	eduInputAPI := eduserver.SetupEDUServerComponent(base, cache.New())
	if !base.UseHTTPAPIs {
		base.SetEDUServer(eduInputAPI)
	}

	asAPI := appservice.SetupAppServiceAPIComponent(base, transactions.New())
	if !base.UseHTTPAPIs {
		base.SetAppserviceAPI(asAPI)
	}

	fsAPI := federationsender.SetupFederationSenderComponent(base)
	if !base.UseHTTPAPIs {
		base.SetFederationSender(fsAPI)
	}
	rsAPI.SetFederationSenderAPI(fsAPI)

	clientapi.SetupClientAPIComponent(base, transactions.New())

	keyserver.SetupKeyServerComponent(base)
	eduProducer := producers.NewEDUServerProducer(base.EDUServer())
	federationapi.SetupFederationAPIComponent(base, eduProducer)
	mediaapi.SetupMediaAPIComponent(base)
	publicroomsapi.SetupPublicRoomsAPIComponent(base, nil)
	syncapi.SetupSyncAPIComponent(base)

	internal.SetupHTTPAPI(
		http.DefaultServeMux,
		base.PublicAPIMux,
		base.InternalAPIMux,
		cfg,
		base.UseHTTPAPIs,
	)

	// Expose the matrix APIs directly rather than putting them under a /api path.
	go func() {
		serv := http.Server{
			Addr:         *httpBindAddr,
			WriteTimeout: setup.HTTPServerTimeout,
		}

		logrus.Info("Listening on ", serv.Addr)
		logrus.Fatal(serv.ListenAndServe())
	}()
	// Handle HTTPS if certificate and key are provided
	if *certFile != "" && *keyFile != "" {
		go func() {
			serv := http.Server{
				Addr:         *httpsBindAddr,
				WriteTimeout: setup.HTTPServerTimeout,
			}

			logrus.Info("Listening on ", serv.Addr)
			logrus.Fatal(serv.ListenAndServeTLS(*certFile, *keyFile))
		}()
	}

	// We want to block forever to let the HTTP and HTTPS handler serve the APIs
	select {}
}
