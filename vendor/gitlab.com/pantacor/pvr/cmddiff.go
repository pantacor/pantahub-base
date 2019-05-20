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
	"fmt"
	"os"

	"github.com/urfave/cli"
)

func CommandDiff() cli.Command {
	return cli.Command{
		Name:        "diff",
		Aliases:     []string{"d"},
		ArgsUsage:   "[files]",
		Usage:       "Display diff of pristine to working state.",
		Description: "show json diff of working dir to last committed state. filter diff by files if requested.",
		Action: func(c *cli.Context) error {
			wd, err := os.Getwd()
			if err != nil {
				return cli.NewExitError(err, 1)
			}

			pvr, err := NewPvr(c.App, wd)
			if err != nil {
				return cli.NewExitError(err, 2)
			}

			jsonDiff, err := pvr.Diff()
			if err != nil {
				return cli.NewExitError(err, 3)
			}

			jsonDiffF, err := FormatJson(*jsonDiff)

			if err != nil {
				cli.NewExitError(err, 4)
			}
			fmt.Println(string(jsonDiffF))

			return nil
		},
	}
}
