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

	"github.com/urfave/cli"
	"gitlab.com/pantacor/pvr/libpvr"
)

func CommandInit() cli.Command {
	return cli.Command{
		Name:        "init",
		Aliases:     []string{"i"},
		ArgsUsage:   "",
		Usage:       "pvr'ize the working directory",
		Description: "Creates the .pvr according to default spec. Creates systemc if not exists.",
		Action: func(c *cli.Context) error {
			wd, err := os.Getwd()
			if err != nil {
				return cli.NewExitError(err, 1)
			}

			session, err := libpvr.NewSession(c.App)

			if err != nil {
				return cli.NewExitError(err, 4)
			}

			pvr, err := libpvr.NewPvr(session, wd)
			if err != nil {
				return cli.NewExitError(err, 2)
			}

			// empty template as starting point; XXX; add Flag to pass custom json
			err = pvr.Init(c.String("objects"))

			if err != nil {
				cli.NewExitError(err, 3)
			}
			return nil
		},
		Flags: []cli.Flag{
			cli.StringFlag{
				Name:  "objects, o",
				Usage: "Use `OBJECTS` directory for storing the file objects. Can be absolute or relative to working directory.",
			},
		},
	}
}
