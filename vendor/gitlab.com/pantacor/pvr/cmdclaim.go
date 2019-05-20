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
	"net/url"
	"os"

	"github.com/urfave/cli"
)

func CommandClaim() cli.Command {
	return cli.Command{
		Name:        "claim",
		Aliases:     []string{"cl"},
		ArgsUsage:   "<device-endpoint> - Endpoint for Device to claim,\n                                 e.g. https://api.pantahub.com/devices/xxxxxxxxxx",
		Usage:       "Claim ownership of a device through challenge",
		Description: "Use a secret challenge only accessible to device owners to claim\n   ownership of a device that has registered itself with pantahub\n\n. Will prompt for challenge if not provided as argument.",
		Flags: []cli.Flag{
			cli.StringFlag{Name: "c, challenge", Usage: "Secret Challenge"},
		},
		Action: func(c *cli.Context) error {
			wd, err := os.Getwd()
			if err != nil {
				return cli.NewExitError(err, 1)
			}

			pvr, err := NewPvr(c.App, wd)
			if err != nil {
				return cli.NewExitError(err, 2)
			}

			var deviceEndpoint string

			if c.NArg() != 1 {
				return cli.NewExitError("You must specify a device to claim. See --help", 3)
			} else {
				deviceEndpoint = c.Args()[0]
			}

			u, err := url.Parse(deviceEndpoint)

			// if no scheme, we assume its device-id/nick
			if u.Scheme == "" {
				u.Scheme = "https"
				u.Host = "api.pantahub.com"
				u.Path = "/devices/" + deviceEndpoint
				deviceEndpoint = u.String()
			}
			challenge := c.String("challenge")

			err = pvr.doClaim(deviceEndpoint, challenge)

			if err != nil {
				return cli.NewExitError("Error claiming device: "+err.Error(), 4)
			}

			return nil
		},
	}
}
