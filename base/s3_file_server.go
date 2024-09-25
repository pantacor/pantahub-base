//
// Copyright 2019  Pantacor Ltd.
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

package base

import (
	"context"
	"crypto/sha256"
	"crypto/tls"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net"
	"net/http"
	"net/url"
	"os"
	"path"
	"path/filepath"
	"strings"
	"time"

	"gitlab.com/pantacor/pantahub-base/objects"
	"gitlab.com/pantacor/pantahub-base/s3"
	"gitlab.com/pantacor/pantahub-base/utils"
	"go.opentelemetry.io/contrib/instrumentation/net/http/otelhttp"
	"gopkg.in/resty.v1"
)

// selectedRegionConfig selected region configuration
var selectedRegionConfig *s3.ConnectionParameters

// ApiRegion k8s api region
var apiRegion string

// S3FileServer s3 file server definition
type S3FileServer struct {
	s3       s3.S3
	regionS3 s3.S3
}

// NewS3FileServer create new s3 file server
func NewS3FileServer() *S3FileServer {
	connParams := s3.ConnectionParameters{
		AccessKey: utils.GetEnv(utils.EnvPantahubS3AccessKeyID),
		SecretKey: utils.GetEnv(utils.EnvPantahubS3SecretAccessKeyID),
		Region:    utils.GetEnv(utils.EnvPantahubS3Region),
		Bucket:    utils.GetEnv(utils.EnvPantahubS3Bucket),
		Endpoint:  utils.GetEnv(utils.EnvPantahubS3Endpoint),
	}

	server := &S3FileServer{
		s3: s3.New(context.TODO(), connParams),
	}

	if selectedRegionConfig != nil {
		server.regionS3 = s3.New(context.TODO(), *selectedRegionConfig)
	}

	return server
}

// WriteCounter counts the number of bytes written to it.
type WriteCounter struct {
	Total int64 // Total # of bytes written
}

// Write implements the io.Writer interface.
//
// Always completes and never returns an error.
func (wc *WriteCounter) Write(p []byte) (int, error) {
	n := len(p)
	wc.Total += int64(n)
	return n, nil
}

func (s *S3FileServer) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	dirName := filepath.Dir(r.URL.Path)
	fileBase := filepath.Base(r.URL.Path)

	tok, err := objects.NewFromValidToken(fileBase)
	if err != nil {
		msg := "Invalid local-s3 request (" + fileBase + "): " + err.Error()
		log.Println(msg)
		utils.HttpErrorWrapper(w, msg, http.StatusForbidden)
		return
	}

	objClaims := tok.Token.Claims.(*objects.ObjectAccessClaims)
	storageID := objClaims.Audience
	p, _ := url.Parse(path.Join(dirName, storageID))
	r.URL = r.URL.ResolveReference(p)
	defer r.Body.Close()

	finalName, err := utils.MakeLocalS3PathForName(storageID)
	if err != nil {
		msg := "ERROR: creating filepath for write: " + err.Error()
		log.Println(msg)
		utils.HttpErrorWrapper(w, msg, http.StatusInternalServerError)
		return
	}

	ctx := context.WithoutCancel(r.Context())
	if r.Method == "GET" {
		if objClaims.Method != http.MethodGet {
			msg := "Invalid objClaims Method; not GET (" + objClaims.Method + ")"
			log.Println(msg)
			utils.HttpErrorWrapper(w, msg, http.StatusForbidden)
			return
		}

		w.Header().Add("Content-Disposition", "attachment; filename=\""+objClaims.DispositionName+"\"")
		w.Header().Add("Content-Length", fmt.Sprintf("%d", objClaims.Size))

		var s3resp *http.Response
		downloadUrl := ""
		region := ""

		// If there is a selected region config load the downloadUrl
		if selectedRegionConfig != nil {
			downloadUrl, err = s.regionS3.DownloadURL(ctx, finalName)
			if err != nil {
				msg := fmt.Sprintf("ERROR: getting download url, %v", err)
				log.Println(msg)
				utils.HttpErrorWrapper(w, msg, http.StatusInternalServerError)
				return
			}

			s3resp, err = http.Get(downloadUrl)
			if err != nil {
				msg := fmt.Sprintf("ERROR: requesting download file, %v\n", err)
				log.Println(msg)
				utils.HttpErrorWrapper(w, msg, http.StatusInternalServerError)
				return
			}
			region = s.regionS3.GetConnectionParams(ctx).Region
		}

		// If the object is not found on the selected region try in default region
		if downloadUrl == "" || (s3resp != nil && s3resp.StatusCode == http.StatusNotFound) {
			downloadUrl, err = s.s3.DownloadURL(ctx, finalName)
			if err != nil {
				msg := fmt.Sprintf("ERROR: getting download url, %v", err)
				log.Println(msg)
				utils.HttpErrorWrapper(w, msg, http.StatusInternalServerError)
				return
			}

			s3resp, err = http.Get(downloadUrl)
			if err != nil {
				msg := fmt.Sprintf("ERROR: requesting download file, %v\n", err)
				log.Println(msg)
				utils.HttpErrorWrapper(w, msg, http.StatusInternalServerError)
				return
			}
			region = s.s3.GetConnectionParams(ctx).Region
		}

		if s3resp.StatusCode != http.StatusOK {
			msg := fmt.Sprintf("ERROR: unexpected response from s3 server, status code %v\ndownloadUrl: %s\n", s3resp.StatusCode, downloadUrl)
			log.Println(msg)
			utils.HttpErrorWrapper(w, msg, s3resp.StatusCode)
			return
		}

		w.Header().Add("PantahubCallTraceRegion", fmt.Sprintf("api=%s; data:%s", apiRegion, region))
		io.CopyN(w, s3resp.Body, objClaims.Size)
		return
	}

	if objClaims.Method != http.MethodPut {
		msg := "Invalid objClaims Method; not PUT (" + objClaims.Method + ")"
		// log.Println(msg)
		utils.HttpErrorWrapper(w, msg, http.StatusForbidden)
		return
	}

	if objClaims.Sha == "" {
		msg := "Invalid objClaims Method; no Sha included"
		log.Println(msg)
		utils.HttpErrorWrapper(w, msg, http.StatusBadRequest)
		return
	}

	tempName := "_part_" + path.Base(finalName)
	preSignedURL, err := s.s3.UploadURL(ctx, tempName)
	if err != nil {
		msg := fmt.Sprintf("ERROR: failed to generate upload url, %v\n", err)
		log.Println(msg)
		utils.HttpErrorWrapper(w, msg, http.StatusInternalServerError)
		return
	}
	// log.Printf("preSignedURL %s sha %s\n", preSignedURL, objClaims.Sha)

	// avoid body close for later sha256 calc
	hasher := sha256.New()
	countWriter := &WriteCounter{}
	intermediateBody := io.TeeReader(r.Body, hasher)
	s3Body := io.TeeReader(intermediateBody, countWriter)

	// storageID SHAONLY means that we only validate the sha for user
	// we introduced this to keep old pvr clients backward compatible
	// that dont understand about LINK semantic when doing a --force
	// post... to ensure old behaviour persists we will do just sha
	// validation, but not persist on disk, otherwise mimicking for
	// consumer the same behaviour
	if storageID == "SHAONLY" {
		buf := make([]byte, 1024*64)

		// lets read all to get sha through hasher ...
		for {
			_, err := s3Body.Read(buf)
			if err != nil {
				break
			}
		}

		sha := hasher.Sum(nil)
		shaS := hex.EncodeToString(sha)

		if shaS != objClaims.Sha {
			msg := fmt.Sprintf("WARNING: file upload sha mismatch with claim: "+shaS+" != "+objClaims.Sha+" readbytes=%d\n", countWriter.Total)
			log.Println(msg)
			utils.HttpErrorWrapper(w, msg, http.StatusBadRequest)
			return
		}

	} else {
		// s3req, err := http.NewRequest(http.MethodPut, preSignedURL, s3Body)
		// if err != nil {
		// 	msg := fmt.Sprintf("ERROR: failed to generate s3 request, %v\n", err)
		// 	log.Println(msg)
		// 	utils.HttpErrorWrapper(w, msg, http.StatusInternalServerError)
		// 	return
		// }

		// transport := &http.Transport{
		// 	Proxy: http.ProxyFromEnvironment,
		// 	DialContext: (&net.Dialer{
		// 		Timeout:   60 * time.Minute,
		// 		KeepAlive: 30 * time.Second,
		// 		DualStack: true,
		// 	}).DialContext,
		// 	MaxIdleConns:          100,
		// 	IdleConnTimeout:       90 * time.Second,
		// 	TLSHandshakeTimeout:   30 * time.Second,
		// 	ExpectContinueTimeout: 15 * time.Second,
		// }

		// httpClient := &http.Client{Transport: transport}
		// if os.Getenv("OTEL_EXPORTER_OTLP_ENDPOINT") != "" {
		// 	httpClient = &http.Client{Transport: }
		// }
		// s3resp, err := httpClient.Do(s3req)

		httpClient := resty.New()
		// httpClient.Debug = true

		t := &http.Transport{
			Proxy: http.ProxyFromEnvironment,
			DialContext: (&net.Dialer{
				Timeout:   60 * time.Minute,
				KeepAlive: 30 * time.Second,
				DualStack: true,
			}).DialContext,
			MaxIdleConns:          100,
			IdleConnTimeout:       90 * time.Second,
			TLSHandshakeTimeout:   30 * time.Second,
			ExpectContinueTimeout: 15 * time.Second,
		}
		if os.Getenv("OTEL_EXPORTER_OTLP_ENDPOINT") != "" {
			transport := otelhttp.NewTransport(t)
			httpClient.SetTransport(transport)
		} else {
			httpClient.SetTransport(t)
		}

		s3req := httpClient.R().
			SetBody(s3Body).
			SetHeader("Content-Type", "application/octet-stream").
			SetContentLength(true)
		s3resp, err := s3req.Put(preSignedURL)
		if err != nil {
			defer s.s3.Delete(ctx, tempName)
			msg := fmt.Sprintf("ERROR: failed to upload to %s\n", preSignedURL)
			log.Println(msg)
			utils.HttpErrorWrapper(w, msg, http.StatusInternalServerError)
			return
		}

		if s3resp.StatusCode() != http.StatusOK {
			body := s3resp.Body()
			msg := fmt.Sprintf("ERROR: unexpected response from remote S3 server")
			if err != nil {
				msg = fmt.Sprintf("ERROR: unexpected response from remote S3 server %d -- %s", s3resp.StatusCode(), err.Error())
			} else {
				msg = fmt.Sprintf("ERROR: remote S3 server %d -- %s", s3resp.StatusCode(), body)
			}
			log.Println(msg)
			utils.HttpErrorWrapper(w, msg, http.StatusInternalServerError)
			return
		}

		sha := hasher.Sum(nil)
		shaS := hex.EncodeToString(sha)

		if shaS != objClaims.Sha {
			msg := fmt.Sprintf("WARNING: file upload sha mismatch with claim: "+shaS+" != "+objClaims.Sha+" readbytes=%d\n", countWriter.Total)
			log.Println(msg)
			utils.HttpErrorWrapper(w, msg, http.StatusBadRequest)
			return
		}

		err = s.s3.Rename(ctx, tempName, finalName)
		if err != nil {
			msg := fmt.Sprintf("ERROR: failed to commit s3 upload, %v\n", err)
			log.Println(msg)
			utils.HttpErrorWrapper(w, msg, http.StatusInternalServerError)
			return
		}
	}

	w.WriteHeader(http.StatusOK)
}

func LoadDynamicS3ByRegion() error {
	if utils.GetEnv(utils.EnvPantahubS3RegionSelection) != "k8s" ||
		utils.GetEnv(utils.EnvPantahubStorageDriver) != "s3" {
		return nil
	}

	fmt.Println("parsing s3 from k8s -- stating")

	token, err := os.ReadFile("/run/secrets/kubernetes.io/serviceaccount/token")
	if err != nil {
		return fmt.Errorf("token file can't be read %s -- %s", "/run/secrets/kubernetes.io/serviceaccount/token", err)
	}

	if utils.GetEnv(utils.EnvPantahubS3RegionalConfigMap) == "{}" &&
		utils.GetEnv(utils.EnvK8sNodeName) == "" &&
		utils.GetEnv(utils.EnvK8sApiUrl) == "" {
		return fmt.Errorf(
			"some environment variables are missing in order to start the auto region selection: \n %s: %s \n %s: %s \n %s: %s",
			utils.EnvPantahubS3RegionalConfigMap, utils.GetEnv(utils.EnvPantahubS3RegionalConfigMap),
			utils.EnvK8sNodeName, utils.GetEnv(utils.EnvK8sNodeName),
			utils.EnvK8sApiUrl, utils.GetEnv(utils.EnvK8sApiUrl),
		)
	}

	response := map[string]interface{}{}

	client := resty.New()
	client.SetTLSClientConfig(&tls.Config{InsecureSkipVerify: true})
	res, err := client.R().
		SetHeader("Authorization", "Bearer "+string(token)).
		Get(fmt.Sprintf("%s/api/v1/nodes/%s", utils.GetEnv(utils.EnvK8sApiUrl), utils.GetEnv(utils.EnvK8sNodeName)))
	if err != nil {
		return err
	}

	if err = json.Unmarshal(res.Body(), &response); err != nil {
		return err
	}

	metadata, ok := response["metadata"].(map[string]interface{})
	if !ok {
		return fmt.Errorf("can not find metada on k8s response: %s", res.Body())
	}

	labels, ok := metadata["labels"].(map[string]interface{})
	if !ok {
		return fmt.Errorf("can not find labels on k8s response: %s", res.Body())
	}

	apiRegion = ""
	for key := range labels {
		if strings.Contains(key, "region-") {
			values := strings.SplitAfter(key, "node-role.kubernetes.io/region-")
			if len(values) == 2 {
				apiRegion = values[1]
			}
		}
	}

	if apiRegion == "" {
		fmt.Printf("parsing s3 from k8s -- \"node-role.kubernetes.io/region-*\" is not present on node metadata.labels, defaulting to %s \n", utils.GetEnv(utils.EnvPantahubS3Region))
		fmt.Printf("parsing s3 from k8s -- %s, defaulting to %s \n", utils.EnvPantahubS3Endpoint, utils.GetEnv(utils.EnvPantahubS3Endpoint))
		return nil
	} else {
		fmt.Printf("parsing s3 from k8s -- k8s cluster running in region %s \n", apiRegion)
	}

	config, err := s3.GetCPFromJsonByRegion(utils.GetEnv(utils.EnvPantahubS3RegionalConfigMap), apiRegion)
	if err != nil {
		fmt.Printf("parsing s3 from k8s -- can't parse PANTAHUB_S3_CONFIG_MAP, defaulting to %s \n", utils.GetEnv(utils.EnvPantahubS3Endpoint))
		fmt.Printf("parsing s3 from k8s -- region, defaulting to %s \n", utils.GetEnv(utils.EnvPantahubS3Region))
		fmt.Printf("parsing s3 from k8s -- r%s \n", err)
		return nil
	}

	if config == nil {
		fmt.Printf("parsing s3 from k8s -- The PANTAHUB_S3_CONFIG_MAP is empty or doesn't have configuration for %s \n", apiRegion)
		return nil
	}

	selectedRegionConfig = config

	fmt.Println("parsing s3 from k8s -- success")
	fmt.Printf("parsing s3 from k8s -- selected storage: %s \n", selectedRegionConfig.Endpoint)

	return nil
}

func GetSelectedRegionConfig() *s3.ConnectionParameters {
	return selectedRegionConfig
}
