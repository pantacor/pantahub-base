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

func CommandImport() cli.Command {
	return cli.Command{
		Name:        "import",
		Aliases:     []string{"i"},
		ArgsUsage:   "<repo-tarball>",
		Usage:       "import repo tarball (like the one produced by 'pvr export') into pvr in current working dir",
		Description: "can import files with.gz or .tgz extension as well as plain .tar. Will not do pvr checkout, so working directory stays untouched.",
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

			err = pvr.Import(c.Args()[0])
			if err != nil {
				return cli.NewExitError(err, 3)
			}

			return nil
		},
	}
}
