//
// Copyright 2017  Pantacor Ltd.
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
package main

import (
	"os"

	"crypto/tls"

	"github.com/go-resty/resty"
	"github.com/urfave/cli"
)

func main() {

	app := cli.NewApp()
	app.Name = "pvr"
	app.Usage = "PantaVisor Repo"
	app.Version = "0.0.1"

	app.Flags = []cli.Flag{
		cli.StringFlag{
			Name:  "auth, a",
			Usage: "Use `ACCESS_TOKEN` for authorization with core services",
		},
		cli.StringFlag{
			Name:  "baseurl, b",
			Usage: "Use `BASEURL` for resolving prn URIs to core service endpoints",
		},
	}

	app.Before = func(c *cli.Context) error {
		c.App.Metadata["PANTAHUB_BASE"] = "https://pantahub.appspot.com/api"
		if os.Getenv("PANTAHUB_BASE") != "" {
			c.App.Metadata["PANTAHUB_BASE"] = os.Getenv("PANTAHUB_BASE")
		}
		if c.GlobalString("baseurl") != "" {
			c.App.Metadata["PANTAHUB_BASE"] = c.GlobalString("baseurl")
		}
		c.App.Metadata["PANTAHUB_AUTH"] = ""
		if os.Getenv("PANTAHUB_AUTH") != "" {
			c.App.Metadata["PANTAHUB_AUTH"] = os.Getenv("PANTAHUB_AUTH")
		}
		if c.GlobalString("auth") != "" {
			c.App.Metadata["PANTAHUB_AUTH"] = c.GlobalString("auth")
		}
		// XXX: make a --no-verify flag instead of thisr
		resty.SetTLSClientConfig(&tls.Config{InsecureSkipVerify: true})

		return nil
	}

	app.Commands = []cli.Command{
		CommandInit(),
		CommandAdd(),
		CommandJson(),
		CommandDiff(),
		CommandStatus(),
		CommandCommit(),
		CommandPut(),
		CommandPost(),
		CommandGet(),
		CommandReset(),
		CommandClone(),
		CommandPutObjects(),
		CommandExport(),
		CommandImport(),
	}
	app.Run(os.Args)
}
