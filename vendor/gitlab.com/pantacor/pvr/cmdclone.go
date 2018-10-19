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
	"path"

	"github.com/urfave/cli"
	"gitlab.com/pantacor/pvr/libpvr"
)

func CommandClone() cli.Command {
	return cli.Command{
		Name:        "clone",
		Aliases:     []string{"c"},
		ArgsUsage:   "<repository> [directory]",
		Usage:       "clone repository to a new target directory",
		Description: "this combines operations: new, get, checkout",
		Action: func(c *cli.Context) error {
			wd, err := os.Getwd()
			if err != nil {
				return cli.NewExitError(err, 1)
			}

			if c.NArg() < 1 {
				return cli.NewExitError("clone needs need repository argument. See --help", 2)
			}

			newUrl, err := url.Parse(c.Args().Get(0))
			if err != nil {
				return cli.NewExitError(err, 3)
			}

			base := path.Base(newUrl.Path)
			base = path.Join(wd, base)
			if c.NArg() == 2 {
				base = c.Args().Get(1)
			}

			err = os.Mkdir(base, 0755)
			if err != nil {
				return cli.NewExitError(err, 4)
			}

			session, err := libpvr.NewSession(c.App)

			if err != nil {
				return cli.NewExitError(err, 4)
			}

			pvr, err := libpvr.NewPvr(session, base)
			if err != nil {
				return cli.NewExitError(err, 2)
			}

			err = pvr.Init(c.String("objects"))
			if err != nil {
				return cli.NewExitError(err, 6)
			}

			err = pvr.GetRepo(newUrl.String(), false)
			if err != nil {
				return cli.NewExitError(err, 7)
			}

			err = pvr.Reset()

			if err != nil {
				return cli.NewExitError(err, 8)
			}

			return nil
		},
		Flags: []cli.Flag{
			cli.StringFlag{
				Name:  "objects, o",
				Usage: "Use `OBJECTS` directory for storing the file objects. Can be absolue or relative to working directory.",
			},
		},
	}

}
