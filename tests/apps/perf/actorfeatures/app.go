/*
Copyright 2023 The Dapr Authors
Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at
    http://www.apache.org/licenses/LICENSE-2.0
Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"strconv"
	"strings"
	"sync/atomic"

	"github.com/dapr/dapr/tests/e2e/utils"
	chi "github.com/go-chi/chi/v5"
)

const (
	appPort                 = 3000
	daprHTTPPort            = 3500
	daprV1URL               = "http://localhost:3500/v1.0"
	actorMethodURLFormat    = daprV1URL + "/actors/%s/%s/%s/%s"
	actorSaveStateURLFormat = daprV1URL + "/actors/%s/%s/state/"
	actorGetStateURLFormat  = daprV1URL + "/actors/%s/%s/state/%s/"
	defaultActorType        = "testactorfeatures"   // Actor type must be unique per test app.
	actorTypeEnvName        = "TEST_APP_ACTOR_TYPE" // Env variable tests can set to change actor type.
	actorIdleTimeout        = "1h"
	actorScanInterval       = "30s"
	drainOngoingCallTimeout = "30s"
	drainRebalancedActors   = true
	secondsToWaitInMethod   = 5
)

type daprConfig struct {
	Entities                []string `json:"entities,omitempty"`
	ActorIdleTimeout        string   `json:"actorIdleTimeout,omitempty"`
	ActorScanInterval       string   `json:"actorScanInterval,omitempty"`
	DrainOngoingCallTimeout string   `json:"drainOngoingCallTimeout,omitempty"`
	DrainRebalancedActors   bool     `json:"drainRebalancedActors,omitempty"`
}

var registeredActorType = map[string]bool{}

var daprConfigResponse = daprConfig{
	getActorType(),
	actorIdleTimeout,
	actorScanInterval,
	drainOngoingCallTimeout,
	drainRebalancedActors,
}

// request for timer or reminder.
type timerReminderRequest struct {
	OldName   string `json:"oldName,omitempty"`
	ActorType string `json:"actorType,omitempty"`
	ActorID   string `json:"actorID,omitempty"`
	NewName   string `json:"newName,omitempty"`
	Data      string `json:"data,omitempty"`
	DueTime   string `json:"dueTime,omitempty"`
	Period    string `json:"period,omitempty"`
	TTL       string `json:"ttl,omitempty"`
	Callback  string `json:"callback,omitempty"`
}

// response object from an actor invocation request
type daprActorResponse struct {
	Data     []byte            `json:"data"`
	Metadata map[string]string `json:"metadata"`
}

// requestResponse represents a request or response for the APIs in this app.
type response struct {
	ActorType string `json:"actorType,omitempty"`
	ActorID   string `json:"actorId,omitempty"`
	Method    string `json:"method,omitempty"`
	StartTime int    `json:"start_time,omitempty"`
	EndTime   int    `json:"end_time,omitempty"`
	Message   string `json:"message,omitempty"`
}

func getActorType() []string {
	actorType := os.Getenv(actorTypeEnvName)
	if actorType == "" {
		registeredActorType[defaultActorType] = true
		return []string{defaultActorType}
	}

	actorTypes := strings.Split(actorType, ",")
	for _, tp := range actorTypes {
		registeredActorType[tp] = true
	}
	return actorTypes
}

var httpClient = utils.NewHTTPClient(true)
var counter = atomic.Int32{}

// indexHandler is the handler for root path
func indexHandler(w http.ResponseWriter, r *http.Request) {
	log.Println("indexHandler is called")
	w.WriteHeader(http.StatusOK)
}

func configHandler(w http.ResponseWriter, r *http.Request) {
	log.Printf("Processing Dapr request for %s, responding with %#v", r.URL.RequestURI(), daprConfigResponse)
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	json.NewEncoder(w).Encode(daprConfigResponse)
}

func actorMethodHandler(w http.ResponseWriter, r *http.Request) {
	log.Printf("Processing actor method request for %s", r.URL.RequestURI())
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	counter.Add(1)
}

func deactivateActorHandler(w http.ResponseWriter, r *http.Request) {
	log.Printf("Processing %s actor request for %s", r.Method, r.URL.RequestURI())
	actorType := chi.URLParam(r, "actorType")

	if !registeredActorType[actorType] {
		log.Printf("Unknown actor type: %s", actorType)
		w.WriteHeader(http.StatusBadRequest)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
}

// calls Dapr's Actor method/timer/reminder: simulating actor client call.
func testCallActorHandler(w http.ResponseWriter, r *http.Request) {
	log.Printf("Processing %s test request for %s", r.Method, r.URL.RequestURI())

	actorType := chi.URLParam(r, "actorType")
	id := chi.URLParam(r, "id")
	callType := chi.URLParam(r, "callType")
	method := chi.URLParam(r, "method")

	url := fmt.Sprintf(actorMethodURLFormat, actorType, id, callType, method)

	log.Printf("Invoking: %s %s\n", r.Method, url)
	expectedHTTPCode := 200
	var req timerReminderRequest
	switch callType {
	case "method":
		// NO OP
	case "timers":
		fallthrough
	case "reminders":
		if r.Method == http.MethodGet {
			expectedHTTPCode = 200
		} else {
			expectedHTTPCode = 204
		}
		body, err := io.ReadAll(r.Body)
		defer r.Body.Close()
		if err != nil {
			log.Printf("Could not get reminder request: %s", err.Error())
			return
		}

		log.Println("Body data: " + string(body))
		json.Unmarshal(body, &req)
	}

	body, err := httpCall(r.Method, url, req, expectedHTTPCode)
	if err != nil {
		log.Printf("Could not read actor's test response: %s", err.Error())
		w.WriteHeader(http.StatusInternalServerError)
		return
	}

	if len(body) == 0 {
		w.WriteHeader(http.StatusOK)
		return
	}

	var response daprActorResponse
	err = json.Unmarshal(body, &response)
	if err != nil {
		log.Printf("Could not parse actor's test response: %s", err.Error())
		w.WriteHeader(http.StatusInternalServerError)
		return
	}

	w.Write(response.Data)
}

func counterHandler(w http.ResponseWriter, r *http.Request) {
	log.Printf("Processing %s test request for %s", r.Method, r.URL.RequestURI())
	if r.Method == "DELETE" {
		counter.Store(0)
	}

	w.WriteHeader(http.StatusOK)
	w.Write([]byte(strconv.Itoa(int(counter.Load()))))
}

func healthzHandler(w http.ResponseWriter, r *http.Request) {
	w.WriteHeader(http.StatusOK)
	w.Write([]byte(""))
}

func httpCall(method string, url string, requestBody interface{}, expectedHTTPStatusCode int) ([]byte, error) {
	var body []byte
	var err error

	if requestBody != nil {
		body, err = json.Marshal(requestBody)
		if err != nil {
			return nil, err
		}
	}

	req, err := http.NewRequest(method, url, bytes.NewReader(body))
	if err != nil {
		return nil, err
	}

	res, err := httpClient.Do(req)
	if err != nil {
		return nil, err
	}

	defer res.Body.Close()

	if res.StatusCode != expectedHTTPStatusCode {
		var errBody []byte
		errBody, err = io.ReadAll(res.Body)
		if err == nil {
			return nil, fmt.Errorf("Expected http status %d, received %d, payload ='%s'", expectedHTTPStatusCode, res.StatusCode, string(errBody)) //nolint:stylecheck
		}

		return nil, fmt.Errorf("Expected http status %d, received %d", expectedHTTPStatusCode, res.StatusCode) //nolint:stylecheck
	}

	resBody, err := io.ReadAll(res.Body)
	if err != nil {
		return nil, err
	}

	return resBody, nil
}

// appRouter initializes restful api router
func appRouter() http.Handler {
	router := chi.NewRouter()

	router.Get("/", indexHandler)
	router.Get("/dapr/config", configHandler)

	// The POST method is used to register reminder
	// The DELETE method is used to unregister reminder
	router.HandleFunc("/call/{actorType}/{id}/{callType}/{method}", testCallActorHandler)

	router.Put("/actors/{actorType}/{id}/method/{method}", actorMethodHandler)
	router.Put("/actors/{actorType}/{id}/method/{reminderOrTimer}/{method}", actorMethodHandler)

	router.Post("/actors/{actorType}/{id}", deactivateActorHandler)
	router.Delete("/actors/{actorType}/{id}", deactivateActorHandler)

	router.Get("/healthz", healthzHandler)

	router.Get("/counter", counterHandler)
	router.Delete("/counter", counterHandler)

	router.HandleFunc("/test", fortioTestHandler)

	return router
}

func main() {
	log.Printf("Actor App - listening on http://localhost:%d", appPort)

	log.Fatal(http.ListenAndServe(fmt.Sprintf(":%d", appPort), appRouter()))
}
