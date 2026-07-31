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

package util

// CurrentCloudRegion selects the cloud endpoints: 0 is mainland China, 1 is North America. This English-first build
// defaults to North America; InitConf overwrites it with the value persisted in conf.json.
var CurrentCloudRegion = 1

func IsChinaCloud() bool {
	return 0 == CurrentCloudRegion
}

func GetCloudServer() string {
	if 0 == CurrentCloudRegion {
		return chinaServer
	}
	return northAmericaServer
}

func GetCloudWebSocketServer() string {
	if 0 == CurrentCloudRegion {
		return chinaWebSocketServer
	}
	return northAmericaWebSocketServer
}

func GetCloudSyncServer() string {
	if 0 == CurrentCloudRegion {
		return chinaSyncServer
	}
	return northAmericaSyncServer
}

func GetCloudAssetsServer() string {
	if 0 == CurrentCloudRegion {
		return chinaCloudAssetsServer
	}
	return northAmericaCloudAssetsServer
}

func GetCloudAccountServer() string {
	if 0 == CurrentCloudRegion {
		return chinaAccountServer
	}
	return northAmericaAccountServer
}

func GetCloudForumAssetsServer() string {
	if 0 == CurrentCloudRegion {
		return chinaForumAssetsServer
	}
	return northAmericaForumAssetsServer
}

const (
	chinaServer            = "https://siyuan-sync.b3logfile.com"    // Mainland China cloud service address, Alibaba Cloud load balancer, used for the API (data sync file upload/download goes through Qiniu Cloud OSS ChinaSyncServer)
	chinaWebSocketServer   = "wss://siyuan-sync.b3logfile.com"      // Mainland China cloud WebSocket service address, Alibaba Cloud load balancer
	chinaSyncServer        = "https://siyuan-data.b3logfile.com/"   // Mainland China cloud data sync service address, Qiniu Cloud OSS, used for data sync file upload/download
	chinaCloudAssetsServer = "https://assets.b3logfile.com/siyuan/" // Mainland China cloud image hosting service address, used to render subscriber-only images in export preview mode
	chinaAccountServer     = "https://ld246.com"                    // Mainland China Liandi service address, used for account login and sharing/publishing posts
	chinaForumAssetsServer = "https://b3logfile.com/file/"          // Mainland China Liandi image hosting service address, used for publishing articles to the community

	northAmericaServer            = "https://siyuan-cloud.liuyun.io"   // North America cloud service address, Cloudflare, used for the API (data sync file upload/download goes through Qiniu Cloud OSS northAmericaSyncServer)
	northAmericaWebSocketServer   = "wss://siyuan-cloud.liuyun.io"     // North America cloud WebSocket service address, Cloudflare
	northAmericaSyncServer        = "https://siyuan-data.liuyun.io/"   // North America cloud data sync service address, Qiniu Cloud OSS, used for data sync file upload/download
	northAmericaCloudAssetsServer = "https://assets.liuyun.io/siyuan/" // North America cloud image hosting service address, used to render subscriber-only images in export preview mode
	northAmericaAccountServer     = "https://liuyun.io"                // Liuyun service address, used for account login and sharing/publishing posts
	northAmericaForumAssetsServer = "https://assets.liuyun.io/file/"   // North America cloud image hosting service address, used for publishing articles to the community

	BazaarStatServer = "https://bazaar.b3logfile.com" // Bazaar package stats service address, Qiniu Cloud, global CDN
	BazaarOSSServer  = "https://oss.b3logfile.com"    // Cloud object storage address, Qiniu Cloud, used only for reading bazaar packages, global CDN
)
