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
	"fmt"

	"github.com/urfave/cli"
	"gitlab.com/pantacor/pvr/libpvr"
)

func CommandRegister() cli.Command {
	return cli.Command{
		Name:        "register",
		Aliases:     []string{"reg"},
		ArgsUsage:   "[pantahub-url]",
		Usage:       "register new user account with pantahub",
		Description: "register with confirmation mail",
		Flags: []cli.Flag{
			cli.StringFlag{Name: "u, user", Usage: "Desired Username"},
			cli.StringFlag{Name: "p, pass", Usage: "Password"},
			cli.StringFlag{Name: "e, email", Usage: "email to use for user"},
		},
		Action: func(c *cli.Context) error {

			u := c.String("user")
			p := c.String("pass")
			e := c.String("email")

			aEp := "https://api.pantahub.com"

			if c.NArg() > 1 {
				return cli.NewExitError("Only one argument (pantahub url) allowed. See --help.", 2)
			} else if c.NArg() == 1 {
				aEp = c.Args()[0]
			}

			err := libpvr.DoRegister(aEp, e, u, p)

			if err != nil {
				return cli.NewExitError("Error Registering User: "+err.Error(), 3)
			}

			fmt.Println("User '" + u + "' registered. Follow email instructions sent to '" + e + "' before you can log in.")

			return nil
		},
	}
}
