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

type User struct {
	UserId                          string       `json:"userId"`
	UserName                        string       `json:"userName"`
	UserAvatarURL                   string       `json:"userAvatarURL"`
	UserHomeBImgURL                 string       `json:"userHomeBImgURL"`
	UserTitles                      []*UserTitle `json:"userTitles"`
	UserIntro                       string       `json:"userIntro"`
	UserNickname                    string       `json:"userNickname"`
	UserCreateTime                  string       `json:"userCreateTime"`
	UserSiYuanProExpireTime         float64      `json:"userSiYuanProExpireTime"`
	UserToken                       string       `json:"userToken"`
	UserTokenExpireTime             string       `json:"userTokenExpireTime"`
	UserSiYuanRepoSize              float64      `json:"userSiYuanRepoSize"`
	UserSiYuanPointExchangeRepoSize float64      `json:"userSiYuanPointExchangeRepoSize"`
	UserSiYuanAssetSize             float64      `json:"userSiYuanAssetSize"`
	UserTrafficUpload               float64      `json:"userTrafficUpload"`
	UserTrafficDownload             float64      `json:"userTrafficDownload"`
	UserTrafficAPIGet               float64      `json:"userTrafficAPIGet"`
	UserTrafficAPIPut               float64      `json:"userTrafficAPIPut"`
	UserTrafficTime                 float64      `json:"userTrafficTime"`
	UserSiYuanSubscriptionPlan      float64      `json:"userSiYuanSubscriptionPlan"`   // -1: not subscribed, 0: standard subscription, 1: education subscription, 2: trial subscription
	UserSiYuanSubscriptionStatus    float64      `json:"userSiYuanSubscriptionStatus"` // -1: not subscribed, 0: subscription available, 1: subscription banned, 2: subscription expired
	UserSiYuanSubscriptionType      float64      `json:"userSiYuanSubscriptionType"`   // 0 annual subscription; 1 lifetime subscription; 2 monthly subscription (not currently supported)
	UserSiYuanOneTimePayStatus      float64      `json:"userSiYuanOneTimePayStatus"`   // 0 feature not paid for; 1 feature paid for
}

type UserTitle struct {
	Name string `json:"name"`
	Desc string `json:"desc"`
	Icon string `json:"icon"`
}

func (user *User) GetCloudRepoAvailableSize() int64 {
	return int64(user.UserSiYuanRepoSize - user.UserSiYuanAssetSize)
}
