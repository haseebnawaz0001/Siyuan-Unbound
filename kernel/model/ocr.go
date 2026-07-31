package model

import (
	"path/filepath"
	"strings"
	"time"

	"github.com/siyuan-note/logging"
	"github.com/siyuan-note/siyuan/kernel/cache"
	"github.com/siyuan-note/siyuan/kernel/sql"
	"github.com/siyuan-note/siyuan/kernel/task"
	"github.com/siyuan-note/siyuan/kernel/util"
)

func OCRAssetsJob() {
	util.WaitForTesseractInit()

	if !util.TesseractEnabled {
		return
	}

	task.AppendTaskWithTimeout(task.OCRImage, 30*time.Second, autoOCRAssets)
}

func autoOCRAssets() {
	if !util.TesseractEnabled {
		return
	}

	defer logging.Recover()

	assetsPath := util.GetDataAssetsAbsPath()
	assets := getUnOCRAssetsAbsPaths()
	if 0 < len(assets) {
		for i, assetAbsPath := range assets {
			text := util.GetOcrJsonText(util.Tesseract(assetAbsPath))
			p := strings.TrimPrefix(assetAbsPath, assetsPath)
			p = "assets" + filepath.ToSlash(p)
			util.SetAssetText(p, text)
			if 7 <= i { // Process at most 7 images per task to avoid tying up system resources for too long
				break
			}
		}
	}

	util.CleanNotExistAssetsTexts()

	// Flush OCR results to the database
	util.NodeOCRQueueLock.Lock()
	defer util.NodeOCRQueueLock.Unlock()
	for _, id := range util.NodeOCRQueue {
		sql.IndexNodeQueue(id)
	}
	util.NodeOCRQueue = nil
}

func getUnOCRAssetsAbsPaths() (ret []string) {
	// Only get assets that need OCR
	ocrAssets := cache.FilterAssets(func(path string, asset *cache.Asset) bool {
		return util.IsTesseractExtractable(asset.Path)
	})

	assetsPath := util.GetDataAssetsAbsPath()
	for _, asset := range ocrAssets {
		// Skip assets that already have OCR text
		if util.ExistsAssetText(asset.Path) {
			continue
		}
		absPath := filepath.Join(assetsPath, strings.TrimPrefix(asset.Path, "assets"))
		// Assets in encrypted notebooks are ciphertext, so skip OCR (avoids garbage text from encrypted images polluting the search index)
		if IsEncryptedAssetPath(absPath) {
			continue
		}
		ret = append(ret, absPath)
	}
	return
}

func FlushAssetsTextsJob() {
	util.SaveAssetsTexts()
}
