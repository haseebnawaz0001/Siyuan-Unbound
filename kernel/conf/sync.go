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

type Sync struct {
	CloudName           string  `json:"cloudName"`           // Cloud sync directory name
	Enabled             bool    `json:"enabled"`             // Whether sync is enabled
	Perception          bool    `json:"perception"`          // Whether perception is enabled
	Mode                int     `json:"mode"`                // Sync mode, 0: unset (converted to 1 in initConf for compatibility with existing configs), 1: automatic, 2: manual https://github.com/siyuan-note/siyuan/issues/5089, 3: fully manual https://github.com/siyuan-note/siyuan/issues/7295
	Interval            int     `json:"interval"`            // Auto sync interval, in seconds
	Synced              int64   `json:"synced"`              // Most recent sync time
	Stat                string  `json:"stat"`                // Most recent sync stats info
	GenerateConflictDoc bool    `json:"generateConflictDoc"` // Whether to generate a conflict document on cloud sync conflict
	Provider            int     `json:"provider"`            // Cloud storage service provider
	S3CloudNameMigrated bool    `json:"s3CloudNameMigrated"` // Whether the one-shot S3 sync directory name reset has already run, see conf.InitConf
	S3                  *S3     `json:"s3"`                  // S3 object storage service configuration
	WebDAV              *WebDAV `json:"webdav"`              // WebDAV service configuration
	Local               *Local  `json:"local"`               // Local file system service configuration
}

func NewSync() *Sync {
	return &Sync{
		CloudName:  "main",
		Enabled:    false,
		Perception: false,
		Mode:       1,
		// Default to keeping a visible copy of the losing side of a conflict. Block-level merging resolves edits to
		// different blocks automatically, so a conflict now means the same block really was edited on two devices, and
		// leaving that copy only in the history directory makes it easy to lose work without noticing.
		GenerateConflictDoc: true,
		Provider:            ProviderSiYuan,
		// A fresh config has never had a bucket name written into CloudName, so the migration must not fire for it.
		S3CloudNameMigrated: true,
		Interval:            30,
	}
}

type S3 struct {
	Endpoint       string `json:"endpoint"`       // Service endpoint
	AccessKey      string `json:"accessKey"`      // Access Key
	SecretKey      string `json:"secretKey"`      // Secret Key
	Bucket         string `json:"bucket"`         // Bucket
	Region         string `json:"region"`         // Storage region
	PathStyle      bool   `json:"pathStyle"`      // Whether to use path-style addressing
	SkipTlsVerify  bool   `json:"skipTlsVerify"`  // Whether to skip TLS verification
	Timeout        int    `json:"timeout"`        // Timeout, in seconds
	ConcurrentReqs int    `json:"concurrentReqs"` // Number of concurrent requests
}

type WebDAV struct {
	Endpoint       string `json:"endpoint"`       // Service endpoint
	Username       string `json:"username"`       // Username
	Password       string `json:"password"`       // Password
	SkipTlsVerify  bool   `json:"skipTlsVerify"`  // Whether to skip TLS verification
	Timeout        int    `json:"timeout"`        // Timeout, in seconds
	ConcurrentReqs int    `json:"concurrentReqs"` // Number of concurrent requests
}

type Local struct {
	Endpoint       string `json:"endpoint"`       // Service endpoint (local file system directory)
	Timeout        int    `json:"timeout"`        // Timeout, in seconds
	ConcurrentReqs int    `json:"concurrentReqs"` // Number of concurrent requests
}

const (
	ProviderSiYuan = 0 // ProviderSiYuan is the cloud storage service officially provided by SiYuan
	ProviderS3     = 2 // ProviderS3 is the cloud storage service provided via the S3 protocol object storage
	ProviderWebDAV = 3 // ProviderWebDAV is the cloud storage service provided via the WebDAV protocol
	ProviderLocal  = 4 // ProviderLocal is the storage service provided by the local file system
)

func ProviderToStr(provider int) string {
	switch provider {
	case ProviderSiYuan:
		return "SiYuan"
	case ProviderS3:
		return "S3"
	case ProviderWebDAV:
		return "WebDAV"
	case ProviderLocal:
		return "Local File System"
	}
	return "Unknown"
}
