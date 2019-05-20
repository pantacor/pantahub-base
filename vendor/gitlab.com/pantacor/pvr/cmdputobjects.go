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
)

func CommandPutObjects() cli.Command {
	return cli.Command{
		Name:      "putobjects",
		Aliases:   []string{"po"},
		ArgsUsage: "[objects-endpoint]",
		Usage:     "put objects from local repository to objects-endpoint",
		Action: func(c *cli.Context) error {
			wd, err := os.Getwd()
			if err != nil {
				return cli.NewExitError(err, 1)
			}

			if c.NArg() != 1 {
				return cli.NewExitError("Push requires exactly 1 argument. See --help.", 2)
			}

			pvr, err := NewPvr(c.App, wd)
			if err != nil {
				return cli.NewExitError(err, 3)
			}

			err = pvr.PutObjects(c.Args()[0], c.Bool("force"))
			if err != nil {
				return cli.NewExitError(err, 4)
			}

			return nil
		},
		Flags: []cli.Flag{
			cli.BoolFlag{
				Name:  "force, f",
				Usage: "force reupload of existing objects",
			},
		},
	}
}
