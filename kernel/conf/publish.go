// SiYuan - Refactor your thinking
// Copyright (c) 2020-present, b3log.org
//
// This program is free software: you can redistribute it and/or modify
// it under the terms of the GNU Affero General Public License as published by
// the Free Software Foundation, either version 3 of the License, or
// (at your option) any later version.
//
// This program is distributed in the hope that it will be useful,
// but WITHOUT ANY WARRANTY; without even the implied warranty of
// MERCHANTABILITY or FITNESS FOR A PARTICULAR PURPOSE.  See the
// GNU Affero General Public License for more details.
//
// You should have received a copy of the GNU Affero General Public License
// along with this program.  If not, see <https://www.gnu.org/licenses/>.

package conf

type Publish struct {
	Enable bool       `json:"enable"` // Whether the publish service is enabled
	Port   uint16     `json:"port"`   // Publish service port
	Auth   *BasicAuth `json:"auth"`   // Basic auth
}

type BasicAuth struct {
	Enable   bool                `json:"enable"`   // Whether basic auth is enabled
	Accounts []*BasicAuthAccount `json:"accounts"` // Account list
}

type BasicAuthAccount struct {
	Username string `json:"username"` // Username
	Password string `json:"password"` // Password
	Memo     string `json:"memo"`     // Memo
}

func NewPublish() *Publish {
	return &Publish{
		Enable: false,
		Port:   6808,
		Auth: &BasicAuth{
			Enable:   true,
			Accounts: []*BasicAuthAccount{},
		},
	}
}
