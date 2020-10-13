//
// Copyright 2020  Pantacor Ltd.
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//   http://www.apache.org/licenses/LICENSE-2.0
//
//   Unless required by applicable law or agreed to in writing, software
//   distributed under the License is distributed on an "AS IS" BASIS,
//   WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
//   See the License for the specific language governing permissions and
//   limitations under the License.
//
package utils

import (
	"bufio"
	"net"
	"net/http"

	"github.com/ant0ine/go-json-rest/rest"
	cjson "github.com/gibson042/canonicaljson-go"
)

type CanonicalJSONMiddleware struct{}

// MiddlewareFunc makes RecorderMiddleware implement the Middleware interface.
// Inspired by IndentJSONMiddleware by go-json-rest
func (mw *CanonicalJSONMiddleware) MiddlewareFunc(h rest.HandlerFunc) rest.HandlerFunc {

	return func(w rest.ResponseWriter, r *rest.Request) {

		writer := &canonicalJsonResponseWriter{w, false}
		// call the wrapped handler
		h(writer, r)
	}
}

type canonicalJsonResponseWriter struct {
	rest.ResponseWriter
	wroteHeader bool
}

func (w *canonicalJsonResponseWriter) EncodeJson(v interface{}) ([]byte, error) {
	json, err := cjson.Marshal(v)
	if err != nil {
		return nil, err
	}
	return json, nil
}

func (w *canonicalJsonResponseWriter) WriteJson(v interface{}) error {
	b, err := w.EncodeJson(v)
	if err != nil {
		return err
	}
	_, err = w.Write(b)
	if err != nil {
		return err
	}
	return nil
}

func (w *canonicalJsonResponseWriter) WriteHeader(code int) {
	w.ResponseWriter.Header().Add("PhJsonFormat", "gibson042-canonicaljson")
	w.ResponseWriter.WriteHeader(code)
	w.wroteHeader = true
}

func (w *canonicalJsonResponseWriter) Flush() {
	if !w.wroteHeader {
		w.WriteHeader(http.StatusOK)
	}
	flusher := w.ResponseWriter.(http.Flusher)
	flusher.Flush()
}

func (w *canonicalJsonResponseWriter) CloseNotify() <-chan bool {
	notifier := w.ResponseWriter.(http.CloseNotifier)
	return notifier.CloseNotify()
}

func (w *canonicalJsonResponseWriter) Hijack() (net.Conn, *bufio.ReadWriter, error) {
	hijacker := w.ResponseWriter.(http.Hijacker)
	return hijacker.Hijack()
}

func (w *canonicalJsonResponseWriter) Write(b []byte) (int, error) {
	if !w.wroteHeader {
		w.WriteHeader(http.StatusOK)
	}
	writer := w.ResponseWriter.(http.ResponseWriter)
	return writer.Write(b)
}
