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
	app.Version = VERSION

	app.Flags = []cli.Flag{
		cli.StringFlag{
			Name:   "access-token, a",
			Usage:  "Use `ACCESS_TOKEN` for authorization with core services",
			EnvVar: "PVR_ACCESSTOKEN",
		},
		cli.StringFlag{
			Name:   "baseurl, b",
			Usage:  "Use `BASEURL` for resolving prn URIs to core service endpoints",
			EnvVar: "PVR_BASEURL",
		},
		cli.BoolFlag{
			Name:   "debug, d",
			Usage:  "enable debugging output for rest calls",
			EnvVar: "PVR_DEBUG",
		},
		cli.BoolFlag{
			Name:   "insecure, i",
			Usage:  "skip tls verify",
			EnvVar: "PVR_INSECURE",
		},
	}

	app.Before = func(c *cli.Context) error {
		resty.SetTLSClientConfig(&tls.Config{InsecureSkipVerify: c.GlobalBool("insecure")})
		resty.SetDebug(c.GlobalBool("debug"))

		c.App.Metadata["PVR_AUTH"] = c.GlobalString("auth")

		if c.GlobalString("baseurl") != "" {
			c.App.Metadata["PVR_BASEURL"] = c.GlobalString("baseurl")
		} else {
			c.App.Metadata["PVR_BASEURL"] = "https://api.pantahub.com"
		}

		return nil
	}

	app.Commands = []cli.Command{
		CommandInit(),
		CommandAdd(),
		CommandJson(),
		CommandClaim(),
		CommandDiff(),
		CommandStatus(),
		CommandCommit(),
		CommandPut(),
		CommandPost(),
		CommandGet(),
		CommandMerge(),
		CommandReset(),
		CommandClone(),
		CommandPutObjects(),
		CommandExport(),
		CommandImport(),
		CommandRegister(),
		CommandScan(),
		CommandPs(),
	}
	app.Run(os.Args)
}
