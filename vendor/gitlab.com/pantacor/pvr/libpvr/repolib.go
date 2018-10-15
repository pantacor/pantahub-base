//
// Copyright 2017, 2018  Pantacor Ltd.
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
package libpvr

import (
	"archive/tar"
	"bufio"
	"compress/gzip"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/ioutil"
	"log"
	"net/http"
	"net/url"
	"os"
	"path"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/asac/json-patch"
	"github.com/cavaliercoder/grab"
	"github.com/go-resty/resty"

	"golang.org/x/crypto/ssh/terminal"

	"gopkg.in/cheggaaa/pb.v1"

	pvrapi "gitlab.com/pantacor/pvr/api"
)

type PvrStatus struct {
	NewFiles       []string
	RemovedFiles   []string
	ChangedFiles   []string
	UntrackedFiles []string
	JsonDiff       *[]byte
}

// stringify of file status for "pvr status" list...
func (p *PvrStatus) String() string {
	str := ""
	for _, f := range p.NewFiles {
		str += "A " + f + "\n"
	}
	for _, f := range p.RemovedFiles {
		str += "D " + f + "\n"
	}
	for _, f := range p.ChangedFiles {
		str += "C " + f + "\n"
	}
	for _, f := range p.UntrackedFiles {
		str += "? " + f + "\n"
	}
	return str
}

type PvrMap map[string]interface{}
type PvrIndex map[string]string

type Pvr struct {
	Initialized     bool
	Dir             string
	Pvrdir          string
	Pvdir           string
	Objdir          string
	Pvrconfig       PvrConfig
	PristineJson    []byte
	PristineJsonMap PvrMap
	NewFiles        PvrIndex
	Session         *Session
}

type PvrConfig struct {
	DefaultGetUrl  string
	DefaultPutUrl  string
	DefaultPostUrl string
	ObjectsDir     string

	// tokens by realm
	AccessTokens  map[string]string
	RefreshTokens map[string]string
}

type WrappableRestyCallFunc func(req *resty.Request) (*resty.Response, error)

func (p *Pvr) String() string {
	return "PVR: " + p.Dir
}

func NewPvr(s *Session, dir string) (*Pvr, error) {
	return NewPvrInit(s, dir)
}

func NewPvrInit(s *Session, dir string) (*Pvr, error) {
	pvr := Pvr{
		Dir:         dir + string(filepath.Separator),
		Pvrdir:      filepath.Join(dir, ".pvr"),
		Pvdir:       filepath.Join(dir, ".pv"),
		Objdir:      filepath.Join(dir, ".pvr", "objects"),
		Initialized: false,
		Session:     s,
	}

	pvr.Pvrconfig.AccessTokens = make(map[string]string)
	pvr.Pvrconfig.RefreshTokens = make(map[string]string)

	fileInfo, err := os.Stat(pvr.Dir)

	if os.IsNotExist(err) {
		err = os.MkdirAll(pvr.Dir, 0600)
	}

	if err != nil {
		return nil, err
	}

	if !fileInfo.IsDir() {
		return nil, errors.New("pvr path is not a directory: " + dir)
	}

	fileInfo, err = os.Stat(filepath.Join(pvr.Pvrdir, "json"))

	if err == nil && fileInfo.IsDir() {
		return nil, errors.New("Repo is in bad state. .pvr/json is a directory")
	}

	if err != nil {
		pvr.Initialized = false
		return &pvr, nil
	}

	byteJson, err := ioutil.ReadFile(filepath.Join(pvr.Pvrdir, "json"))
	// pristine json we keep as string as this will allow users load into
	// convenient structs
	pvr.PristineJson = byteJson

	err = json.Unmarshal(pvr.PristineJson, &pvr.PristineJsonMap)
	if err != nil {
		return nil, errors.New("JSON Unmarshal (" + strings.TrimPrefix(filepath.Join(pvr.Pvrdir, "json"), pvr.Dir) + "): " + err.Error())
	}

	// new files is a json file we will parse happily
	bytesNew, err := ioutil.ReadFile(filepath.Join(pvr.Pvrdir, "new"))
	if err == nil {
		err = json.Unmarshal(bytesNew, &pvr.NewFiles)
	} else {
		pvr.NewFiles = map[string]string{}
		err = nil
	}

	if err != nil {
		return &pvr, errors.New("Repo in bad state. JSON Unmarshal (" + strings.TrimPrefix(filepath.Join(pvr.Pvrdir, "json"), pvr.Dir) + ") Not possible. Make a copy of the repository for forensics, file a bug and maybe delete that file manually to try to recover: " + err.Error())
	}

	fileInfo, err = os.Stat(filepath.Join(pvr.Pvrdir, "config"))

	if err == nil && fileInfo.IsDir() {
		return nil, errors.New("Repo is in bad state. .pvr/json is a directory")
	} else if err == nil {
		byteJson, err := ioutil.ReadFile(filepath.Join(pvr.Pvrdir, "config"))

		err = json.Unmarshal(byteJson, &pvr.Pvrconfig)
		if err != nil {
			return nil, errors.New("JSON Unmarshal (" + strings.TrimPrefix(filepath.Join(pvr.Pvrdir, "json"), pvr.Dir) + "): " + err.Error())
		}
	} else {
		// not exist
	}

	if pvr.Pvrconfig.ObjectsDir != "" {
		pvr.Objdir = pvr.Pvrconfig.ObjectsDir
	}

	return &pvr, nil
}

func (p *Pvr) addPvrFile(path string) error {
	shaBal, err := FiletoSha(path)
	if err != nil {
		return err
	}
	relPath := strings.TrimPrefix(path, p.Dir)
	p.NewFiles[relPath] = shaBal
	return nil
}

// XXX: make this git style
func (p *Pvr) AddFile(globs []string) error {

	err := filepath.Walk(p.Dir, func(walkPath string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}

		if strings.HasPrefix(walkPath, p.Pvrdir) || strings.HasPrefix(walkPath, p.Pvdir) {
			return nil
		}

		// no globs specified: add all
		if len(globs) == 0 || (len(globs) == 1 && globs[0] == ".") {
			p.addPvrFile(walkPath)
		}
		for _, glob := range globs {
			absglob := glob

			if !filepath.IsAbs(absglob) && absglob[0] != '/' {
				absglob = p.Dir + glob
			}
			matched, err := filepath.Match(absglob, walkPath)
			if err != nil {
				fmt.Println("WARNING: cannot read file (" + err.Error() + "):" + walkPath)
				return err
			}
			if matched {
				err = p.addPvrFile(walkPath)
				if err != nil {
					return nil
				}
			}
		}
		return nil
	})

	if err != nil {
		return err
	}

	jsonData, err := json.Marshal(p.NewFiles)
	if err != nil {
		return err
	}

	err = ioutil.WriteFile(filepath.Join(p.Pvrdir, "new.XXX"), jsonData, 0644)
	if err != nil {
		return err
	}
	err = os.Rename(filepath.Join(p.Pvrdir, "new.XXX"), filepath.Join(p.Pvrdir, "new"))
	if err != nil {
		return err
	}
	return nil
}

// create the canonical json for the working directory
func (p *Pvr) GetWorkingJson() ([]byte, []string, error) {

	untrackedFiles := []string{}
	workingJson := map[string]interface{}{}
	workingJson["#spec"] = "pantavisor-multi-platform@1"

	err := filepath.Walk(p.Dir, func(filePath string, info os.FileInfo, err error) error {
		relPath := strings.TrimPrefix(filePath, p.Dir)
		if relPath == "" {
			return nil
		}
		if info.IsDir() {
			return nil
		}
		// ignore .pvr and .pv directories
		if _, ok := p.PristineJsonMap[relPath]; !ok {
			if _, ok1 := p.NewFiles[relPath]; !ok1 {
				if strings.HasPrefix(relPath, ".pvr"+string(os.PathSeparator)) {
					return nil
				}
				if strings.HasPrefix(relPath, ".pv"+string(os.PathSeparator)) {
					return nil
				}
				untrackedFiles = append(untrackedFiles, relPath)
				return nil
			}
		}
		// inline json
		if strings.HasSuffix(filepath.Base(filePath), ".json") {
			jsonFile := map[string]interface{}{}

			data, err := ioutil.ReadFile(filePath)
			if err != nil {
				return err
			}

			err = json.Unmarshal(data, &jsonFile)
			if err != nil {
				return errors.New("JSON Unmarshal (" + strings.TrimPrefix(filePath, p.Dir) + "): " + err.Error())
			}
			workingJson[relPath] = jsonFile
		} else {
			sha, err := FiletoSha(filePath)
			if err != nil {
				return err
			}
			workingJson[relPath] = sha
		}

		return nil
	})

	if err != nil {
		return []byte{}, []string{}, err
	}

	b, err := json.Marshal(workingJson)

	if err != nil {
		return []byte{}, []string{}, err
	}

	return b, untrackedFiles, err
}

func (p *Pvr) Init(objectsDir string) error {

	return p.InitCustom("", objectsDir)
}

func (p *Pvr) InitCustom(customInitJson string, objectsDir string) error {

	var EMPTY_PVR_JSON string = `
{
	"#spec": "pantavisor-multi-platform@1"
}`

	_, err := os.Stat(p.Pvrdir)

	if err == nil {
		return errors.New("pvr init: .pvr directory/file found (" + p.Pvrdir + "). Cannot initialize an existing repository.")
	}

	err = os.Mkdir(p.Pvrdir, 0755)
	if err != nil {
		return err
	}

	// allow overwrite and remember abs path in config
	if objectsDir != "" {
		p.Objdir, err = filepath.Abs(objectsDir)

		if err != nil {
			return errors.New("Unexpected Error 1: " + err.Error())
		}

		p.Pvrconfig.ObjectsDir = p.Objdir
		p.SaveConfig()
	}

	err = os.Mkdir(p.Objdir, 0755)

	jsonFile, err := os.OpenFile(filepath.Join(p.Pvrdir, "json"), os.O_CREATE|os.O_WRONLY, 0644)

	if err != nil {
		return err
	}

	if customInitJson != "" {
		_, err = jsonFile.Write([]byte(customInitJson))
	} else {
		_, err = jsonFile.Write([]byte(EMPTY_PVR_JSON))
	}
	return err
}

func (p *Pvr) Diff() (*[]byte, error) {
	workingJson, _, err := p.GetWorkingJson()
	if err != nil {
		return nil, err
	}

	diff, err := jsonpatch.CreateMergePatch(p.PristineJson, workingJson)

	if err != nil {
		return nil, err
	}

	return &diff, nil
}

func (p *Pvr) Status() (*PvrStatus, error) {
	rs := PvrStatus{}

	workingJson, untrackedFiles, err := p.GetWorkingJson()
	if err != nil {
		return nil, err
	}

	diff, err := jsonpatch.CreateMergePatch(p.PristineJson, workingJson)
	if err != nil {
		return nil, err
	}

	rs.JsonDiff = &diff

	// make json map out of diff
	diffJson := map[string]interface{}{}
	err = json.Unmarshal(*rs.JsonDiff, &diffJson)
	if err != nil {
		return nil, err
	}

	// run
	for file := range diffJson {
		val := diffJson[file]

		if val == nil {
			rs.RemovedFiles = append(rs.RemovedFiles, file)
			continue
		}

		// if we have this key in pristine, then file was changed
		if _, ok := p.PristineJsonMap[file]; ok {
			rs.ChangedFiles = append(rs.ChangedFiles, file)
			continue
		} else {
			rs.NewFiles = append(rs.NewFiles, file)
		}
	}

	rs.UntrackedFiles = untrackedFiles

	return &rs, nil
}

func (p *Pvr) Commit(msg string) error {
	status, err := p.Status()

	if err != nil {
		return err
	}

	for _, v := range status.ChangedFiles {
		fmt.Println("Committing " + filepath.Join(p.Objdir+v))
		if strings.HasSuffix(v, ".json") {
			continue
		}
		sha, err := FiletoSha(v)
		if err != nil {
			return err
		}
		err = Copy(filepath.Join(p.Objdir, sha), v)
		if err != nil {
			return err
		}
	}

	// copy all objects with atomic commit
	for _, v := range status.NewFiles {
		fmt.Println("Adding " + v)
		if strings.HasSuffix(v, ".json") {
			continue
		}
		sha, err := FiletoSha(v)
		if err != nil {
			return err
		}
		_, err = os.Stat(filepath.Join(p.Objdir, sha))
		// if not exists, then copy; otherwise continue
		if err != nil {

			err = Copy(filepath.Join(p.Objdir, sha+".new"), v)
			if err != nil {
				return err
			}
			err = os.Rename(filepath.Join(p.Objdir, sha+".new"),
				path.Join(p.Objdir, sha))
			if err != nil {
				return err
			}
		}
		if err != nil {
			return err
		}
	}

	for _, v := range status.RemovedFiles {
		fmt.Println("Removing " + v)
	}

	ioutil.WriteFile(filepath.Join(p.Pvrdir, "commitmsg.new"), []byte(msg), 0644)
	err = os.Rename(filepath.Join(p.Pvrdir, "commitmsg.new"), filepath.Join(p.Pvrdir, "commitmsg"))
	if err != nil {
		return err
	}

	newJson, err := jsonpatch.MergePatch(p.PristineJson, *status.JsonDiff)

	if err != nil {
		return err
	}
	err = ioutil.WriteFile(filepath.Join(p.Pvrdir, "json.new"), newJson, 0644)

	if err != nil {
		return err
	}

	err = os.Rename(filepath.Join(p.Pvrdir, "json.new"), filepath.Join(p.Pvrdir, "json"))

	if err != nil {
		return err
	}

	// ignore error here as new might not exist
	os.Remove(filepath.Join(p.Pvrdir, "new"))

	return nil
}

func (p *Pvr) PutLocal(repoPath string) error {

	_, err := os.Stat(repoPath)
	if err != os.ErrNotExist {
		err = os.MkdirAll(repoPath, 0755)
	}
	if err != nil {
		return err
	}

	objectsPath := filepath.Join(repoPath, "objects")
	info, err := os.Stat(objectsPath)
	if err == nil && !info.IsDir() {
		return errors.New("PVR repo directory in inusable state (objects is not a directory)")
	} else if err != nil {
		err = os.MkdirAll(objectsPath, 0755)
	}
	if err != nil {
		return err
	}

	// push all objects
	for k := range p.PristineJsonMap {
		if strings.HasSuffix(k, ".json") {
			continue
		}
		v := p.PristineJsonMap[k].(string)
		Copy(filepath.Join(objectsPath, v)+".new", filepath.Join(p.Dir, ".pvr", v))

	}
	err = filepath.Walk(p.Objdir, func(filePath string, info os.FileInfo, err error) error {
		// ignore directories
		if info.IsDir() {
			return nil
		}
		base := path.Base(filePath)
		err = Copy(filepath.Join(objectsPath, base+".new"), filePath)
		if err != nil {
			return err
		}

		err = os.Rename(filepath.Join(objectsPath, base+".new"),
			path.Join(objectsPath, base))
		return err
	})

	err = Copy(filepath.Join(repoPath, "json.new"), filepath.Join(p.Pvrdir, "json"))
	if err != nil {
		return err
	}

	return os.Rename(filepath.Join(repoPath, "json.new"),
		path.Join(repoPath, "json"))
}

type PvrInfo struct {
	jsonUrl string `json:json-url`
	objUrl  string `json:object-url`
}

type Object struct {
	Id         string `json:"id" bson:"id"`
	StorageId  string `json:"storage-id" bson:"_id"`
	Owner      string `json:"owner"`
	ObjectName string `json:"objectname"`
	Sha        string `json:"sha256sum"`
	Size       string `json:"size"`
	MimeType   string `json:"mime-type"`
}

type ObjectWithAccess struct {
	Object       `bson:",inline"`
	SignedPutUrl string `json:"signed-puturl"`
	SignedGetUrl string `json:"signed-geturl"`
	Now          string `json:"now"`
	ExpireTime   string `json:"expire-time"`
}

func (p *Pvr) initializeRemote(repoPath string) (pvrapi.PvrRemote, error) {
	res := pvrapi.PvrRemote{}
	repoUrl, err := url.Parse(repoPath)

	if err != nil {
		return res, err
	}

	pvrRemoteUrl := repoUrl
	pvrRemoteUrl.Path = path.Join(pvrRemoteUrl.Path, ".pvrremote")

	response, err := p.Session.DoAuthCall(func(req *resty.Request) (*resty.Response, error) {
		return req.Get(pvrRemoteUrl.String())
	})

	if err != nil {
		return res, err
	}

	if response.StatusCode() != 200 {
		return res, errors.New("REST call failed. " +
			strconv.Itoa(response.StatusCode()) + "  " + response.Status())
	}

	err = json.Unmarshal(response.Body(), &res)

	if err != nil {
		return res, err
	}

	return res, nil

}

// list all objects reffed by current repo json
func (p *Pvr) listFilesAndObjects() (map[string]string, error) {

	filesAndObjects := map[string]string{}
	// push all objects
	for k, v := range p.PristineJsonMap {
		if strings.HasSuffix(k, ".json") {
			continue
		}
		if strings.HasPrefix(k, "#spec") {
			continue
		}
		objId, ok := v.(string)

		if !ok {
			return map[string]string{}, errors.New("bad object id for file '" + k + "' in pristine pvr json")
		}
		filesAndObjects[k] = objId
	}
	return filesAndObjects, nil
}

func readChallenge(targetPrompt string) string {
	reader := bufio.NewReader(os.Stdin)

	fmt.Println("*** Claim with challenge @ " + targetPrompt + " ***")
	fmt.Print("Enter Challenge: ")
	challenge, _ := reader.ReadString('\n')

	return strings.TrimRight(challenge, "\n")
}

func readCredentials(targetPrompt string) (string, string) {
	reader := bufio.NewReader(os.Stdin)

	fmt.Println("*** Login (/type [R] to register) @ " + targetPrompt + " ***")
	fmt.Print("Username: ")
	username, _ := reader.ReadString('\n')

	username = strings.TrimSpace(username)

	if username == "REGISTER" || username == "REG" || username == "R" {
		return "REGISTER", ""
	}

	fmt.Print("Password: ")
	bytePassword, err := terminal.ReadPassword(int(syscall.Stdin))
	fmt.Println("*****")
	if err != nil {
		fmt.Println("Error: " + err.Error())
	}
	password := string(bytePassword)

	return strings.TrimSpace(username), strings.TrimSpace(password)
}

func readRegistration(targetPrompt string) (string, string, string) {
	reader := bufio.NewReader(os.Stdin)

	fmt.Println("\n*** REGISTER ACCOUNT @ " + targetPrompt + "***")
	fmt.Print(" 1. Email: ")
	email, _ := reader.ReadString('\n')
	fmt.Print(" 2. Username: ")
	username, _ := reader.ReadString('\n')

	password := ""

	for {
		fmt.Print(" 3. Password: ")
		bytePassword, err := terminal.ReadPassword(int(syscall.Stdin))
		fmt.Println("*****")
		if err != nil {
			fmt.Println("Error: " + err.Error())
			continue
		}
		password = string(bytePassword)

		fmt.Print(" 4. Password Repeat: ")
		bytePassword, err = terminal.ReadPassword(int(syscall.Stdin))
		fmt.Println("*****")
		if err != nil {
			fmt.Println("Error: " + err.Error())
			continue
		}
		if password != string(bytePassword) {
			fmt.Println("Passwords do not match. Try again!")
			continue
		}
		password = string(bytePassword)
		break
	}

	return strings.TrimSpace(email), strings.TrimSpace(username), strings.TrimSpace(password)
}

func getWwwAuthenticateInfo(header string) (string, map[string]string) {
	parts := strings.SplitN(header, " ", 2)
	authType := parts[0]
	parts = strings.Split(parts[1], ", ")
	opts := make(map[string]string)

	for _, part := range parts {
		vals := strings.SplitN(part, "=", 2)
		key := vals[0]
		val := strings.Trim(vals[1], "\",")
		opts[key] = val
	}
	return authType, opts
}

func (p *Pvr) DoClaim(deviceEp, challenge string) error {

	u, err := url.Parse(deviceEp)

	if err != nil {
		return errors.New("Parsing device URL failed with err=" + err.Error())
	}

	if challenge == "" {
		challenge = readChallenge(deviceEp)
	}

	uV := u.Query()
	uV.Set("challenge", challenge)
	u.RawQuery = uV.Encode()

	response, err := p.Session.DoAuthCall(func(req *resty.Request) (*resty.Response, error) {
		return req.Put(u.String())
	})

	if err != nil {
		return errors.New("Claiming device failed with err=" + err.Error())
	}

	if response.StatusCode() != http.StatusOK {
		return errors.New("Claiming device failed (code=" + string(response.StatusCode()) + "): " + string(response.Body()))
	}

	return nil
}

type FilePut struct {
	sourceFile string
	putUrl     string
	objName    string
	res        *http.Response
	err        error
	bar        *pb.ProgressBar
}

const (
	PoolSize = 5
)

type AsyncBody struct {
	Delegate io.ReadCloser
	bar      *pb.ProgressBar
}

func (a *AsyncBody) Read(p []byte) (n int, err error) {

	n, err = a.Delegate.Read(p)
	a.bar.Add(n)

	return n, err
}

func worker(jobs chan FilePut, done chan FilePut) {

	for j := range jobs {
		fstat, err := os.Stat(j.sourceFile)
		if err != nil {
			fmt.Errorf("ERROR: " + err.Error())
			continue
		}
		reader, err := os.Open(j.sourceFile)
		if err != nil {
			fmt.Errorf("ERROR: " + err.Error())
			continue
		}

		j.bar.Total = fstat.Size()
		j.bar.Units = pb.U_BYTES
		j.bar.UnitsWidth = 25
		j.bar.ShowSpeed = true
		j.bar.Prefix(filepath.Base(j.objName)[:Min(len(j.objName)-1, 12)] + " ")
		j.bar.ShowCounters = false

		defer reader.Close()
		r := &AsyncBody{
			Delegate: reader,
			bar:      j.bar,
		}

		req, err := http.NewRequest(http.MethodPut, j.putUrl, r)

		if err != nil {
			j.bar.Finish()
			j.err = err
			j.res = nil

			done <- j
			continue
		}

		res, err := http.DefaultClient.Do(req)

		j.bar.ShowFinalTime = true
		j.bar.ShowPercent = false
		j.bar.ShowCounters = false
		j.bar.ShowTimeLeft = false
		j.bar.ShowSpeed = false
		j.bar.ShowBar = true
		j.bar.Set64(j.bar.Total)
		j.bar.UnitsWidth = 25
		if err != nil {
			j.bar.Postfix(" [ERROR]")
		} else {
			j.bar.Postfix(" [OK]")
		}

		j.bar.Finish()

		j.err = err
		j.res = res
		done <- j
	}
}

func (p *Pvr) putFiles(filePut ...FilePut) []FilePut {
	jobs := make(chan FilePut, 100)
	results := make(chan FilePut, 100)

	fileOutPut := []FilePut{}

	pool, err := pb.StartPool()

	if err != nil {
		log.Fatalf("FATAL: starting progressbar pool failed: %s\n", err.Error())
	}

	for i := 0; i < PoolSize; i++ {
		go worker(jobs, results)
	}

	for _, p := range filePut {
		p.bar = pb.New(1)
		pool.Add(p.bar)
		jobs <- p
	}
	close(jobs)

	for i := 0; i < len(filePut); i++ {
		p := <-results
		fileOutPut = append(fileOutPut, p)
		p.bar.Finish()
	}
	close(results)
	pool.Stop()

	return fileOutPut
}

func (p *Pvr) postObjects(pvrRemote pvrapi.PvrRemote, force bool) error {

	filesAndObjects, err := p.listFilesAndObjects()
	if err != nil {
		return err
	}

	filePuts := []FilePut{}

	shaSeen := map[string]interface{}{}

	// push all objects
	for k, v := range filesAndObjects {
		info, err := os.Stat(filepath.Join(p.Objdir, v))
		if err != nil {
			return err
		}
		sizeString := fmt.Sprintf("%d", info.Size())

		remoteObject := ObjectWithAccess{}
		remoteObject.Object.Size = sizeString
		remoteObject.MimeType = "application/octet-stream"
		remoteObject.Sha = v
		remoteObject.ObjectName = k

		uri := pvrRemote.ObjectsEndpointUrl
		if !strings.HasSuffix(uri, "/") {
			uri += "/"
		}

		response, err := p.Session.DoAuthCall(func(req *resty.Request) (*resty.Response, error) {
			return req.SetBody(remoteObject).Post(uri)
		})

		if err != nil {
			return err
		}

		if response == nil {
			return errors.New("BAD STATE; no respo")
		}

		if shaSeen[remoteObject.Sha] != nil {
			_str := remoteObject.ObjectName[0:Min(len(remoteObject.ObjectName)-1, 12)] + " "
			fmt.Println(_str + "[OK - Dupe]")
			continue
		}
		shaSeen[remoteObject.Sha] = "yes"

		if response.StatusCode() != http.StatusOK &&
			response.StatusCode() != http.StatusConflict {
			return errors.New("Error posting object " + strconv.Itoa(response.StatusCode()))
		}

		if response.StatusCode() == http.StatusConflict && !force {
			_str := remoteObject.ObjectName[0:Min(len(remoteObject.ObjectName)-1, 12)] + " "
			fmt.Println(_str + "[OK]")
			continue
		}

		err = json.Unmarshal(response.Body(), &remoteObject)
		if err != nil {
			return err
		}

		fileName := filepath.Join(p.Objdir, v)
		filePuts = append(filePuts, FilePut{
			sourceFile: fileName,
			objName:    remoteObject.ObjectName,
			putUrl:     remoteObject.SignedPutUrl,
		})
	}

	filePutResults := p.putFiles(filePuts...)

	for _, v := range filePutResults {

		if 200 != v.res.StatusCode {
			return errors.New("REST call failed. " +
				strconv.Itoa(v.res.StatusCode) + "  " + v.res.Status)

		}
	}

	return nil
}

func (p *Pvr) PutRemote(repoPath string, force bool) error {

	pvrRemote, err := p.initializeRemote(repoPath)

	if err != nil {
		return err
	}

	err = p.postObjects(pvrRemote, force)

	if err != nil {
		return err
	}

	data, err := ioutil.ReadFile(filepath.Join(p.Pvrdir, "json"))

	if err != nil {
		return err
	}

	uri := pvrRemote.JsonGetUrl
	body := map[string]interface{}{}
	err = json.Unmarshal(data, &body)

	if err != nil {
		return err
	}

	response, err := p.Session.DoAuthCall(func(req *resty.Request) (*resty.Response, error) {
		return req.SetBody(body).Put(uri)
	})

	if err != nil {
		return err
	}

	if 200 != response.StatusCode() {
		return errors.New("REST call failed. " +
			strconv.Itoa(response.StatusCode()) + "  " + response.Status() + "\n\n   " + string(response.Body()))
	}

	err = json.Unmarshal(response.Body(), &body)

	if err != nil {
		return err
	}

	return nil
}

func (p *Pvr) Put(uri string, force bool) error {

	if uri == "" {
		uri = p.Pvrconfig.DefaultPutUrl
	}

	url, err := url.Parse(uri)

	if err != nil {
		return err
	}

	if url.Scheme == "" {
		err = p.PutLocal(uri)
	} else {
		err = p.PutRemote(uri, force)
	}

	if err != nil {
		return err
	}

	if p.Pvrconfig.DefaultGetUrl == "" {
		p.Pvrconfig.DefaultGetUrl = uri
	}
	if p.Pvrconfig.DefaultPutUrl == "" {
		p.Pvrconfig.DefaultPostUrl = uri
	}
	if err == nil {
		p.Pvrconfig.DefaultPutUrl = uri
		err = p.SaveConfig()
	}

	return err
}

func (p *Pvr) SaveConfig() error {
	configNew := filepath.Join(p.Pvrdir, "config.new")
	configPath := filepath.Join(p.Pvrdir, "config")

	byteJson, err := json.Marshal(p.Pvrconfig)
	if err != nil {
		return err
	}
	err = ioutil.WriteFile(configNew, byteJson, 0644)
	if err != nil {
		return err
	}
	err = os.Rename(configNew, configPath)
	return err
}

func (p *Pvr) PutObjects(uri string, force bool) error {
	url, err := url.Parse(uri)

	if err != nil {
		return err
	}

	if url.Scheme == "" {
		return errors.New("not implemented PutObjects Local")
	}

	pvr := pvrapi.PvrRemote{
		ObjectsEndpointUrl: uri,
	}

	return p.postObjects(pvr, force)
}

// make a json post to a REST endpoint. You can provide metainfo etc. in post
// argument as json. postKey if set will be used as key that refers to the posted
// json. Example usage: json blog post, json revision repo with commit message etc
func (p *Pvr) Post(uri string, envelope string, commitMsg string, rev int, force bool) error {

	if uri == "" {
		uri = p.Pvrconfig.DefaultPostUrl
	}
	url, err := url.Parse(uri)

	if err != nil {
		return err
	}

	if url.Scheme == "" {
		return errors.New("Post must be a remote REST endpoint, not: " + url.String())
	}

	remotePvr, err := p.initializeRemote(uri)

	if err != nil {
		return err
	}

	err = p.postObjects(remotePvr, force)

	if err != nil {
		return err
	}

	if envelope == "" {
		envelope = "{}"
	}

	envJson := map[string]interface{}{}
	err = json.Unmarshal([]byte(envelope), &envJson)

	if err != nil {
		return err
	}

	if commitMsg != "" {
		envJson["commit-msg"] = commitMsg
	}

	if rev != 0 {
		envJson["rev"] = rev
	}

	if remotePvr.JsonKey != "" {
		envJson[remotePvr.JsonKey] = p.PristineJsonMap
	} else {
		envJson["post"] = p.PristineJsonMap
	}

	data, err := json.Marshal(envJson)

	if err != nil {
		return err
	}

	response, err := p.Session.DoAuthCall(func(req *resty.Request) (*resty.Response, error) {
		return req.SetBody(data).SetContentLength(true).Post(remotePvr.PostUrl)
	})

	if err != nil {
		return err
	}

	if response.StatusCode() != 200 {
		return errors.New("REST call failed. " +
			strconv.Itoa(response.StatusCode()) + "  " + response.Status() +
			"\n\t" + string(response.Body()))
	}

	fmt.Println("Posted JSON: " + string(response.Body()))

	p.Pvrconfig.DefaultPostUrl = uri
	if p.Pvrconfig.DefaultGetUrl == "" {
		p.Pvrconfig.DefaultGetUrl = uri
	}

	if p.Pvrconfig.DefaultPutUrl == "" {
		p.Pvrconfig.DefaultPutUrl = uri
	}

	err = p.SaveConfig()

	if err != nil {
		fmt.Println("WARNING: couldnt save config " + err.Error())
	}

	return nil
}

func (p *Pvr) GetRepoLocal(repoPath string, merge bool) error {

	rs := map[string]interface{}{}

	// first copy new json, but only rename at the very end after all else succeed
	jsonRepo := filepath.Join(repoPath, "json")

	jsonData, err := ioutil.ReadFile(jsonRepo)
	if err != nil {
		return err
	}

	err = json.Unmarshal(jsonData, &rs)
	if err != nil {
		return errors.New("JSON Unmarshal (json.new):" + err.Error())
	}

	// first copy new json, but only rename at the very end after all else succeed
	configRepo := filepath.Join(repoPath, "config")

	var config *PvrConfig

	configData, err := ioutil.ReadFile(configRepo)
	if err != nil {
		return err
	} else {
		config = new(PvrConfig)
		err = json.Unmarshal(configData, config)
		if err != nil {
			return errors.New("JSON Unmarshal (config.new):" + err.Error())
		}
	}

	for k, v := range rs {
		if strings.HasSuffix(k, ".json") {
			continue
		}
		if strings.HasPrefix(k, "#spec") {
			continue
		}
		getPath := filepath.Join(repoPath, "objects", v.(string))

		// config can overload objectsdir for remote repo; not abs and abs
		if config != nil && config.ObjectsDir != "" {
			if !filepath.IsAbs(config.ObjectsDir) {
				getPath = filepath.Join(repoPath, "..", config.ObjectsDir, v.(string))
			} else {
				getPath = filepath.Join(config.ObjectsDir, v.(string))
			}
		}

		objPathNew := filepath.Join(p.Objdir, v.(string)+".new")
		objPath := filepath.Join(p.Objdir, v.(string))
		fmt.Println("pulling objects file " + getPath + "-> " + objPathNew)
		err := Copy(objPathNew, getPath)
		if err != nil {
			return err
		}
		err = os.Rename(objPathNew, objPath)
		if err != nil {
			return err
		}
	}

	var jsonMerged []byte

	if merge {
		jsonMerged, err = jsonpatch.MergePatch(p.PristineJson, jsonData)
	} else {
		jsonMerged = jsonData
	}

	if err != nil {
		return err
	}

	err = ioutil.WriteFile(filepath.Join(p.Pvrdir, "json.new"), jsonMerged, 0644)

	if err != nil {
		return err
	}

	// all succeeded, atomically commiting the json
	err = os.Rename(filepath.Join(p.Pvrdir, "json.new"), filepath.Join(p.Pvrdir, "json"))

	return err
}

func (p *Pvr) getJsonBuf(pvrRemote pvrapi.PvrRemote) ([]byte, error) {

	response, err := p.Session.DoAuthCall(func(req *resty.Request) (*resty.Response, error) {
		return req.Get(pvrRemote.JsonGetUrl)
	})

	if err != nil {
		return nil, err
	}

	jsonData := response.Body()

	return jsonData, nil
}

func (p *Pvr) grabObjects(requests ...*grab.Request) error {
	client := grab.NewClient()

	client.UserAgent = "PVR client"
	respch := client.DoBatch(4, requests...)

	// start a ticker to update progress every 200ms
	t := time.NewTicker(200 * time.Millisecond)

	progressBars := map[*grab.Request]*pb.ProgressBar{}
	progressBarSlice := make([]*pb.ProgressBar, 0)

	for _, v := range requests {
		shortFile := filepath.Base(v.Filename)[:15]
		progressBars[v] = pb.New(0).Prefix(shortFile)
		progressBars[v].ShowCounters = false
		progressBars[v].SetUnits(pb.KB)
		progressBarSlice = append(progressBarSlice, progressBars[v])
	}

	// monitor downloads
	completed := 0
	inProgress := 0
	responses := make([]*grab.Response, 0)

	pool, err := pb.StartPool(progressBarSlice...)
	if err != nil {
		goto err
	}

	for completed < len(requests) {
		select {
		case resp := <-respch:
			// a new response has been received and has started downloading
			// (nil is received once, when the channel is closed by grab)
			if resp != nil {
				responses = append(responses, resp)
			}

		case <-t.C:
			// update completed downloads
			for i, resp := range responses {
				if resp != nil && resp.IsComplete() {
					req := resp.Request
					// print final result
					if resp.Err() != nil {
						progressBars[req].Finish()
						progressBars[req].ShowBar = false
						progressBars[req].ShowPercent = false
						progressBars[req].ShowCounters = false
						progressBars[req].ShowFinalTime = false
						goto err

					} else {
						progressBars[req].Finish()
						progressBars[req].ShowFinalTime = false
						progressBars[req].ShowPercent = false
						progressBars[req].ShowCounters = false
						progressBars[req].ShowTimeLeft = false
						progressBars[req].ShowBar = false
						progressBars[req].Postfix(" [OK]")
						progressBars[req].Set64(progressBars[req].Total)
					}

					// mark completed
					responses[i] = nil
					completed++
				}
			}

			// update downloads in progress
			inProgress = 0
			for _, resp := range responses {
				if resp != nil {
					req := resp.Request
					inProgress++
					progressBars[req].Total = resp.HTTPResponse.ContentLength
					progressBars[req].ShowTimeLeft = true
					progressBars[req].ShowCounters = true
					progressBars[req].ShowPercent = true
					progressBars[req].Set64(resp.BytesComplete())
					progressBars[req].SetUnits(pb.KB)
				}
			}
		}
	}

err:
	if pool != nil {
		pool.Stop()
	}
	t.Stop()

	return nil
}

func (p *Pvr) getObjects(pvrRemote pvrapi.PvrRemote, jsonData []byte) error {

	jsonMap := map[string]interface{}{}

	err := json.Unmarshal(jsonData, &jsonMap)
	if err != nil {
		return err
	}

	grabs := make([]*grab.Request, 0)
	shaMap := map[string]interface{}{}

	for k := range jsonMap {
		if strings.HasSuffix(k, ".json") {
			continue
		}
		if strings.HasPrefix(k, "#spec") {
			continue
		}

		v := jsonMap[k].(string)

		// only add to downloads if we have not seen this sha already
		if shaMap[v] != nil {
			continue
		} else {
			shaMap[v] = "seen"
		}

		uri := pvrRemote.ObjectsEndpointUrl + "/" + v

		response, err := p.Session.DoAuthCall(func(req *resty.Request) (*resty.Response, error) {
			return req.Get(uri)
		})

		if err != nil {
			return err
		}

		if response.StatusCode() != 200 {
			return errors.New("REST call failed. " +
				strconv.Itoa(response.StatusCode()) + "  " + response.Status())
		}

		remoteObject := ObjectWithAccess{}
		err = json.Unmarshal(response.Body(), &remoteObject)

		if err != nil {
			return err
		}

		req, err := grab.NewRequest(path.Join(p.Objdir, v), remoteObject.SignedGetUrl)
		if err != nil {
			return err
		}

		grabs = append(grabs, req)
	}

	p.grabObjects(grabs...)

	return nil
}

func (p *Pvr) GetRepoRemote(repoPath string, merge bool) error {

	url, err := url.Parse(repoPath)

	if err != nil {
		return err
	}

	if url.Scheme == "" {
		return errors.New("Post must be a remote REST endpoint, not: " + url.String())
	}

	remotePvr, err := p.initializeRemote(repoPath)

	if err != nil {
		return err
	}

	jsonData, err := p.getJsonBuf(remotePvr)

	if err != nil {
		return err
	}

	err = p.getObjects(remotePvr, jsonData)

	if err != nil {
		return err
	}

	var jsonMerged []byte

	if merge {
		jsonMerged, err = jsonpatch.MergePatch(p.PristineJson, jsonData)
	} else {
		jsonMerged = jsonData
	}

	if err != nil {
		return err
	}

	err = ioutil.WriteFile(filepath.Join(p.Pvrdir, "json.new"), jsonMerged, 0644)

	if err != nil {
		return err
	}

	return os.Rename(filepath.Join(p.Pvrdir, "json.new"), filepath.Join(p.Pvrdir, "json"))
}

func (p *Pvr) GetRepo(uri string, merge bool) error {

	if uri == "" {
		uri = p.Pvrconfig.DefaultPutUrl
	}

	url, err := url.Parse(uri)

	if err != nil {
		return err
	}

	p.Pvrconfig.DefaultGetUrl = uri

	if url.Scheme == "" {
		err = p.GetRepoLocal(uri, merge)
	} else {
		if p.Pvrconfig.DefaultPutUrl == "" {
			p.Pvrconfig.DefaultPutUrl = uri
		}

		if p.Pvrconfig.DefaultPostUrl == "" {
			p.Pvrconfig.DefaultPostUrl = uri
		}

		err = p.GetRepoRemote(uri, merge)
	}
	if err != nil {
		return err
	}

	err = p.SaveConfig()

	return err
}

func (p *Pvr) Reset() error {
	data, err := ioutil.ReadFile(filepath.Join(p.Pvrdir, "json"))

	if err != nil {
		return err
	}
	jsonMap := map[string]interface{}{}

	err = json.Unmarshal(data, &jsonMap)

	if err != nil {
		return errors.New("JSON Unmarshal (" +
			strings.TrimPrefix(filepath.Join(p.Pvrdir, "json"), p.Dir) + "): " +
			err.Error())
	}

	for k, v := range jsonMap {
		if strings.HasSuffix(k, ".json") {
			data, err := json.Marshal(v)
			if err != nil {
				return err
			}
			err = ioutil.WriteFile(filepath.Join(p.Dir, k+".new"), data, 0644)
			if err != nil {
				return err
			}
			err = os.Rename(filepath.Join(p.Dir, k+".new"),
				path.Join(p.Dir, k))

		} else if strings.HasPrefix(k, "#spec") {
			continue
		} else {
			objectP := filepath.Join(p.Objdir, v.(string))
			targetP := filepath.Join(p.Dir, k)
			targetD := path.Dir(targetP)
			targetDInfo, err := os.Stat(targetD)
			if err != nil {
				err = os.MkdirAll(targetD, 0755)
			} else if !targetDInfo.IsDir() {
				return errors.New("Not a directory " + targetD)
			}
			if err != nil {
				return err
			}

			err = Copy(targetP+".new", objectP)
			if err != nil {
				return err
			}
			err = os.Rename(targetP+".new", targetP)
			if err != nil {
				return err
			}
		}
	}
	os.Remove(filepath.Join(p.Pvrdir, "new"))
	return nil
}

func addToTar(writer *tar.Writer, archivePath, sourcePath string) error {

	stat, err := os.Stat(sourcePath)

	if err != nil {
		return err
	}

	if stat.IsDir() {
		return errors.New("pvr repo broken state: object file '" + sourcePath + "'is a directory")
	}

	object, err := os.Open(sourcePath)
	if err != nil {
		return err
	}
	defer object.Close()

	header := new(tar.Header)
	header.Name = archivePath
	header.Size = stat.Size()
	header.Mode = int64(stat.Mode())
	header.ModTime = stat.ModTime()

	err = writer.WriteHeader(header)
	if err != nil {
		return err
	}
	_, err = io.Copy(writer, object)
	if err != nil {
		return err
	}

	return nil
}

func (p *Pvr) Export(dst string) error {

	file, err := os.Create(dst)
	if err != nil {
		return err
	}
	defer file.Close()

	var fileWriter io.WriteCloser

	if strings.HasSuffix(strings.ToLower(dst), ".gz") ||
		strings.HasSuffix(strings.ToLower(dst), ".tgz") {

		fileWriter = gzip.NewWriter(file)
		if err != nil {
			return err
		}
		defer fileWriter.Close()
	} else {
		fileWriter = file
	}

	tw := tar.NewWriter(fileWriter)
	defer tw.Close()

	filesAndObjects, err := p.listFilesAndObjects()
	if err != nil {
		return err
	}

	for _, v := range filesAndObjects {
		apath := "objects/" + v
		ipath := filepath.Join(p.Objdir, v)
		err := addToTar(tw, apath, ipath)

		if err != nil {
			return err
		}
	}

	if err := addToTar(tw, "json", filepath.Join(p.Pvrdir, "json")); err != nil {
		return err
	}

	return nil
}

func (p *Pvr) Import(src string) error {
	file, err := os.Open(src)
	if err != nil {
		return err
	}
	defer file.Close()

	var fileReader io.ReadCloser

	if strings.HasSuffix(strings.ToLower(src), ".gz") ||
		strings.HasSuffix(strings.ToLower(src), ".tgz") {

		fileReader, err = gzip.NewReader(file)
		if err != nil {
			return err
		}
		defer fileReader.Close()
	} else {
		fileReader = file
	}

	tw := tar.NewReader(fileReader)

	for {
		header, err := tw.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			return err
		}

		fileInfo := header.FileInfo()

		// we do not make directories as the only directory
		// .pvr/objects must exist in inititialized pvrs
		if fileInfo.IsDir() {
			continue
		}

		filePath := filepath.Join(p.Pvrdir, header.Name)
		filePathNew := filePath + ".new"

		file, err := os.OpenFile(filePathNew, os.O_CREATE|os.O_TRUNC|os.O_WRONLY,
			fileInfo.Mode())
		if err != nil {
			return err
		}
		defer file.Close()

		_, err = io.Copy(file, tw)
		if err != nil {
			return err
		}
		err = os.Rename(filePathNew, filePath)
		if err != nil {
			return err
		}
	}

	return nil
}
