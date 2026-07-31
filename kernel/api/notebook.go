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

package api

import (
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/88250/gulu"
	"github.com/gin-gonic/gin"
	"github.com/siyuan-note/siyuan/kernel/model"
	"github.com/siyuan-note/siyuan/kernel/treenode"
	"github.com/siyuan-note/siyuan/kernel/util"
)

func getNotebookInfo(c *gin.Context) {
	ret := gulu.Ret.NewResult()
	defer c.JSON(http.StatusOK, ret)

	arg, ok := util.JsonArg(c, ret)
	if !ok {
		return
	}

	var boxID string
	if !util.ParseJsonArgs(arg, ret, util.BindJsonArg("notebook", &boxID, true, true)) {
		return
	}
	if util.InvalidIDPattern(boxID, ret) {
		return
	}

	box := model.Conf.Box(boxID)
	if nil == box {
		ret.Code = -1
		ret.Msg = "notebook [" + boxID + "] not found"
		return
	}

	boxInfo := box.GetInfo()
	ret.Data = map[string]any{
		"boxInfo": boxInfo,
	}
}

func setNotebookIcon(c *gin.Context) {
	ret := gulu.Ret.NewResult()
	defer c.JSON(http.StatusOK, ret)

	arg, ok := util.JsonArg(c, ret)
	if !ok {
		return
	}

	var boxID, icon string
	if !util.ParseJsonArgs(arg, ret,
		util.BindJsonArg("notebook", &boxID, true, true),
		util.BindJsonArg("icon", &icon, true, false),
	) {
		return
	}
	model.SetBoxIcon(boxID, icon)
}

func changeSortNotebook(c *gin.Context) {
	ret := gulu.Ret.NewResult()
	defer c.JSON(http.StatusOK, ret)

	arg, ok := util.JsonArg(c, ret)
	if !ok {
		return
	}

	idsArg := arg["notebooks"].([]any)
	var ids []string
	for _, p := range idsArg {
		ids = append(ids, p.(string))
	}
	model.ChangeBoxSort(ids)
}

func renameNotebook(c *gin.Context) {
	ret := gulu.Ret.NewResult()
	defer c.JSON(http.StatusOK, ret)

	arg, ok := util.JsonArg(c, ret)
	if !ok {
		return
	}

	var notebook, name string
	if !util.ParseJsonArgs(arg, ret,
		util.BindJsonArg("notebook", &notebook, true, true),
		util.BindJsonArg("name", &name, true, false),
	) {
		return
	}
	if util.InvalidIDPattern(notebook, ret) {
		return
	}
	err := model.RenameBox(notebook, name)
	if err != nil {
		ret.Code = -1
		ret.Msg = err.Error()
		ret.Data = map[string]any{"closeTimeout": 5000}
		return
	}

	evt := util.NewCmdResult("renamenotebook", 0, util.PushModeBroadcast)
	evt.Data = map[string]any{
		"box":  notebook,
		"name": name,
	}
	util.PushEvent(evt)
}

func removeNotebook(c *gin.Context) {
	ret := gulu.Ret.NewResult()
	defer c.JSON(http.StatusOK, ret)

	arg, ok := util.JsonArg(c, ret)
	if !ok {
		return
	}

	var notebook string
	if !util.ParseJsonArgs(arg, ret, util.BindJsonArg("notebook", &notebook, true, true)) {
		return
	}
	if util.InvalidIDPattern(notebook, ret) {
		return
	}

	if util.ReadOnly && !model.IsUserGuide(notebook) {
		ret.Code = -1
		ret.Msg = model.Conf.Language(34)
		ret.Data = map[string]any{"closeTimeout": 5000}
		return
	}

	err := model.RemoveBox(notebook)
	if err != nil {
		ret.Code = -1
		ret.Msg = err.Error()
		return
	}

	evt := util.NewCmdResult("removeBox", 0, util.PushModeBroadcast)
	evt.Data = map[string]any{
		"box": notebook,
	}
	util.PushEvent(evt)
	model.TriggerOnboardingIfEmpty()
}

func createNotebook(c *gin.Context) {
	ret := gulu.Ret.NewResult()
	defer c.JSON(http.StatusOK, ret)

	arg, ok := util.JsonArg(c, ret)
	if !ok {
		return
	}

	var name string
	if !util.ParseJsonArgs(arg, ret, util.BindJsonArg("name", &name, true, false)) {
		return
	}
	id, err := model.CreateBox(name)
	if err != nil {
		ret.Code = -1
		ret.Msg = err.Error()
		return
	}

	existed, err := model.Mount(id)
	if err != nil {
		ret.Code = -1
		ret.Msg = err.Error()
		return
	}

	box := model.Conf.Box(id)
	if nil == box {
		ret.Code = -1
		ret.Msg = "opened notebook [" + id + "] not found"
		return
	}

	ret.Data = map[string]any{
		"notebook": box,
	}

	evt := util.NewCmdResult("createnotebook", 0, util.PushModeBroadcast)
	evt.Data = map[string]any{
		"box":     box,
		"existed": existed,
	}
	util.PushEvent(evt)
}

func openNotebook(c *gin.Context) {
	ret := gulu.Ret.NewResult()
	defer c.JSON(http.StatusOK, ret)

	arg, ok := util.JsonArg(c, ret)
	if !ok {
		return
	}

	var notebook string
	if !util.ParseJsonArgs(arg, ret, util.BindJsonArg("notebook", &notebook, true, true)) {
		return
	}
	if util.InvalidIDPattern(notebook, ret) {
		return
	}

	isUserGuide := model.IsUserGuide(notebook)
	if util.ReadOnly && !isUserGuide {
		ret.Code = -1
		ret.Msg = model.Conf.Language(34)
		ret.Data = map[string]any{"closeTimeout": 5000}
		return
	}

	msgId := util.PushMsg(model.Conf.Language(45), 1000*60*15)
	defer util.PushClearMsg(msgId)
	existed, err := model.Mount(notebook)
	if err != nil {
		ret.Code = -1
		ret.Msg = err.Error()
		return
	}

	box := model.Conf.Box(notebook)
	if nil == box {
		ret.Code = -1
		ret.Msg = "opened notebook [" + notebook + "] not found"
		return
	}

	evt := util.NewCmdResult("mount", 0, util.PushModeBroadcast)
	evt.Data = map[string]any{
		"box":     box,
		"existed": existed,
	}
	util.PushEvent(evt)

	if isUserGuide {
		appArg := arg["app"]
		app := ""
		if nil != appArg {
			app = appArg.(string)
		}

		go func() {
			var startID string
			i := 0
			for ; i < 70; i++ {
				time.Sleep(100 * time.Millisecond)
				guideStartID := map[string]string{
					"20210808180117-czj9bvb": "20200812220555-lj3enxa",
					"20211226090932-5lcq56f": "20211226115423-d5z1joq",
					"20210808180117-6v0mkxr": "20200923234011-ieuun1p",
					"20240530133126-axarxgx": "20240530101000-4qitucx",
				}
				startID = guideStartID[notebook]
				if treenode.ExistBlockTree(startID) {
					util.BroadcastByTypeAndApp("main", app, "openFileById", 0, "", map[string]any{
						"id": startID,
					})
					break
				}
			}
		}()
	}
}

func closeNotebook(c *gin.Context) {
	ret := gulu.Ret.NewResult()
	defer c.JSON(http.StatusOK, ret)

	arg, ok := util.JsonArg(c, ret)
	if !ok {
		return
	}

	notebook := arg["notebook"].(string)
	if util.InvalidIDPattern(notebook, ret) {
		return
	}
	model.Unmount(notebook)
}

func getNotebookConf(c *gin.Context) {
	ret := gulu.Ret.NewResult()
	defer c.JSON(http.StatusOK, ret)

	arg, ok := util.JsonArg(c, ret)
	if !ok {
		return
	}

	notebook := arg["notebook"].(string)
	if util.InvalidIDPattern(notebook, ret) {
		return
	}

	box := model.Conf.GetBox(notebook)
	if nil == box {
		ret.Code = -1
		ret.Msg = "notebook [" + notebook + "] not found"
		return
	}

	ret.Data = map[string]any{
		"box":  box.ID,
		"name": box.Name,
		"conf": box.GetConf(),
	}
}

func setNotebookConf(c *gin.Context) {
	ret := gulu.Ret.NewResult()
	defer c.JSON(http.StatusOK, ret)

	arg, ok := util.JsonArg(c, ret)
	if !ok {
		return
	}

	notebook := arg["notebook"].(string)
	if util.InvalidIDPattern(notebook, ret) {
		return
	}

	box := model.Conf.GetBox(notebook)
	if nil == box {
		ret.Code = -1
		ret.Msg = "notebook [" + notebook + "] not found"
		return
	}

	param, err := gulu.JSON.MarshalJSON(arg["conf"])
	if err != nil {
		ret.Code = -1
		ret.Msg = err.Error()
		return
	}

	boxConf := box.GetConf()
	// Deep-copy the encryption-related fields, to prevent them from being overwritten when deserializing the request body
	// BoxCrypt is a pointer, and UnmarshalJSON would mutate the same pointed-to object, so it must be deep-copied via the model-layer helper
	savedBoxCrypt := model.DeepCopyBoxEncryption(boxConf.BoxCrypt)
	savedEncrypted := boxConf.Encrypted
	if err = gulu.JSON.UnmarshalJSON(param, boxConf); err != nil {
		ret.Code = -1
		ret.Msg = err.Error()
		return
	}
	boxConf.Encrypted = savedEncrypted
	boxConf.BoxCrypt = savedBoxCrypt

	boxConf.DocCreateSavePath = util.TrimSpaceInPath(boxConf.DocCreateSavePath)

	boxConf.RefCreateSavePath = util.TrimSpaceInPath(boxConf.RefCreateSavePath)

	boxConf.DailyNoteSavePath = util.TrimSpaceInPath(boxConf.DailyNoteSavePath)
	if "" != boxConf.DailyNoteSavePath {
		if !strings.HasPrefix(boxConf.DailyNoteSavePath, "/") {
			boxConf.DailyNoteSavePath = "/" + boxConf.DailyNoteSavePath
		}
	}
	if "/" == boxConf.DailyNoteSavePath {
		ret.Code = -1
		ret.Msg = model.Conf.Language(49)
		return
	}

	boxConf.DailyNoteTemplatePath = util.TrimSpaceInPath(boxConf.DailyNoteTemplatePath)
	if "" != boxConf.DailyNoteTemplatePath {
		if !strings.HasSuffix(boxConf.DailyNoteTemplatePath, ".md") {
			boxConf.DailyNoteTemplatePath += ".md"
		}
		if !strings.HasPrefix(boxConf.DailyNoteTemplatePath, "/") {
			boxConf.DailyNoteTemplatePath = "/" + boxConf.DailyNoteTemplatePath
		}
	}

	if err := box.SaveConf(boxConf); err != nil {
		ret.Code = -1
		ret.Msg = err.Error()
		return
	}
	ret.Data = boxConf
}

func lsNotebooks(c *gin.Context) {
	ret := gulu.Ret.NewResult()
	defer c.JSON(http.StatusOK, ret)

	flashcard := false

	// For backward compatibility with the old API, util.JsonArg() cannot be used directly here
	arg := map[string]any{}
	if err := c.ShouldBindJSON(&arg); err == nil {
		if arg["flashcard"] != nil {
			flashcard = arg["flashcard"].(bool)
		}
	}

	var notebooks []*model.Box
	var publishAccess model.PublishAccess
	isReadOnlyRole := model.IsReadOnlyRoleContext(c)
	if flashcard {
		notebooks = model.GetFlashcardNotebooks()
	} else {
		var err error
		notebooks, err = model.ListNotebooks()
		if err != nil {
			return
		}
		if isReadOnlyRole {
			publishAccess = model.GetPublishAccess()
			tempNotebooks := []*model.Box{}
			for _, notebook := range notebooks {
				// Filter out closed notebooks
				if notebook.Closed {
					continue
				}
				// Filter out notebooks not visible for publishing
				invisible := false
				for _, item := range publishAccess {
					if item.ID == notebook.ID {
						if !item.Visible {
							invisible = true
						}
						break
					}
				}
				if invisible {
					continue
				}
				tempNotebooks = append(tempNotebooks, notebook)
			}
			notebooks = tempNotebooks
		}
	}

	boxDocEnabled := model.IsBoxDocEnabled()
	if !flashcard && boxDocEnabled {
		for _, notebook := range notebooks {
			if !notebook.Closed {
				if isReadOnlyRole {
					notebook.SubFileCount = model.BoxDocSubFileCountForPublish(notebook.ID, publishAccess)
				} else {
					notebook.SubFileCount = model.BoxDocSubFileCount(notebook.ID)
				}
			}
		}
	}

	ret.Data = map[string]any{
		"notebooks":     notebooks,
		"boxDocEnabled": boxDocEnabled,
	}
}

// enableEncryptedNotebooks enables the encrypted notebook feature and sets the master password.
// Enabling it again when already enabled returns an error, to avoid overwriting the existing key parameters.
func enableEncryptedNotebooks(c *gin.Context) {
	ret := gulu.Ret.NewResult()
	defer c.JSON(http.StatusOK, ret)

	arg, ok := util.JsonArg(c, ret)
	if !ok {
		return
	}

	var password string
	if !util.ParseJsonArgs(arg, ret, util.BindJsonArg("password", &password, true, true)) {
		return
	}

	if err := model.EnableEncryptedNotebook(password); err != nil {
		ret.Code = -1
		ret.Msg = err.Error()
		return
	}
}

// disableEncryptedNotebooks disables the encrypted notebook feature. Precondition: no encrypted notebook exists.
func disableEncryptedNotebooks(c *gin.Context) {
	ret := gulu.Ret.NewResult()
	defer c.JSON(http.StatusOK, ret)

	if err := model.DisableEncryptedNotebook(); err != nil {
		ret.Code = -1
		ret.Msg = err.Error()
		return
	}
}

// createEncryptedNotebook creates a new encrypted notebook. Precondition: the encryption feature is enabled.
// The master password must be provided at creation time (used to derive the KEK that wraps the DEK). Once created,
// the kernel has already mounted it atomically.
func createEncryptedNotebook(c *gin.Context) {
	ret := gulu.Ret.NewResult()
	defer c.JSON(http.StatusOK, ret)

	arg, ok := util.JsonArg(c, ret)
	if !ok {
		return
	}

	var name, password string
	if !util.ParseJsonArgs(arg, ret,
		util.BindJsonArg("name", &name, true, false),
		util.BindJsonArg("password", &password, true, true),
	) {
		return
	}

	id, err := model.CreateEncryptedBox(name, password)
	if err != nil {
		ret.Code = -1
		ret.Msg = err.Error()
		return
	}

	// At creation time the DEK is already cached and the encrypted db is already open, so mount directly here;
	// on failure, lock as a rollback to avoid leaving the DEK behind
	existed, err := model.Mount(id)
	if err != nil {
		model.LockBox(id)
		ret.Code = -1
		ret.Msg = err.Error()
		return
	}

	box := model.Conf.GetBox(id)
	evt := util.NewCmdResult("mount", 0, util.PushModeBroadcast)
	evt.Data = map[string]any{
		"box":     box,
		"existed": existed,
	}
	util.PushEvent(evt)

	ret.Data = map[string]any{
		"notebook": box,
	}
}

// unlockNotebook derives the KEK from the master password and decrypts the specified encrypted notebook's DEK,
// caching it in memory.
// Once unlocked, the notebook can be mounted. Each call runs one Argon2id pass (roughly 1 second).
func unlockNotebook(c *gin.Context) {
	ret := gulu.Ret.NewResult()
	defer c.JSON(http.StatusOK, ret)

	arg, ok := util.JsonArg(c, ret)
	if !ok {
		return
	}

	var notebook, password string
	if !util.ParseJsonArgs(arg, ret,
		util.BindJsonArg("notebook", &notebook, true, true),
		util.BindJsonArg("password", &password, true, true),
	) {
		return
	}

	if util.InvalidIDPattern(notebook, ret) {
		return
	}

	boxCrypt, err := model.GetBoxEncryption(notebook)
	if err != nil {
		ret.Code = -1
		ret.Msg = model.Conf.Language(318)
		return
	}
	if boxCrypt == nil || len(boxCrypt.WrappedDEK) == 0 {
		ret.Code = -1
		ret.Msg = model.Conf.Language(319)
		return
	}

	if err := model.UnlockBox(notebook, password, boxCrypt); err != nil {
		ret.Code = -1
		ret.Msg = err.Error()
		return
	}
}

// unlockAndOpenNotebook atomically unlocks and mounts an encrypted notebook: it calls Mount immediately after
// UnlockBox succeeds; if Mount fails, it rolls back with LockBox (clearing the DEK), to avoid the inconsistent
// state of the DEK remaining in memory while the notebook stays unmounted.
func unlockAndOpenNotebook(c *gin.Context) {
	ret := gulu.Ret.NewResult()
	defer c.JSON(http.StatusOK, ret)

	arg, ok := util.JsonArg(c, ret)
	if !ok {
		return
	}

	var notebook, password string
	if !util.ParseJsonArgs(arg, ret,
		util.BindJsonArg("notebook", &notebook, true, true),
		util.BindJsonArg("password", &password, true, true),
	) {
		return
	}

	if util.InvalidIDPattern(notebook, ret) {
		return
	}

	boxCrypt, err := model.GetBoxEncryption(notebook)
	if err != nil {
		ret.Code = -1
		ret.Msg = model.Conf.Language(318)
		return
	}
	if boxCrypt == nil || len(boxCrypt.WrappedDEK) == 0 {
		ret.Code = -1
		ret.Msg = model.Conf.Language(319)
		return
	}

	if err := model.UnlockBox(notebook, password, boxCrypt); err != nil {
		ret.Code = -1
		ret.Msg = err.Error()
		return
	}

	// Mount immediately after a successful unlock; on failure, roll back by locking to clear the DEK and avoid leftovers
	msgId := util.PushMsg(model.Conf.Language(45), 1000*60*15)
	defer util.PushClearMsg(msgId)
	existed, err := model.Mount(notebook)
	if err != nil {
		model.LockBox(notebook)
		ret.Code = -1
		ret.Msg = err.Error()
		return
	}

	box := model.Conf.Box(notebook)
	if nil == box {
		model.LockBox(notebook)
		ret.Code = -1
		ret.Msg = "opened notebook [" + notebook + "] not found"
		return
	}

	evt := util.NewCmdResult("mount", 0, util.PushModeBroadcast)
	evt.Data = map[string]any{
		"box":     box,
		"existed": existed,
	}
	util.PushEvent(evt)
}

// lockNotebook locks the specified encrypted notebook: it clears the cached DEK and unmounts it.
func lockNotebook(c *gin.Context) {
	ret := gulu.Ret.NewResult()
	defer c.JSON(http.StatusOK, ret)

	arg, ok := util.JsonArg(c, ret)
	if !ok {
		return
	}

	var notebook string
	if !util.ParseJsonArgs(arg, ret, util.BindJsonArg("notebook", &notebook, true, true)) {
		return
	}

	if util.InvalidIDPattern(notebook, ret) {
		return
	}

	if !model.IsEncryptedBox(notebook) {
		ret.Code = -1
		ret.Msg = model.Conf.Language(319)
		return
	}

	// Unmount's internal unmount0 already clears the DEK and closes the encrypted db, so a separate LockBox call isn't needed.
	// Conversely, calling LockBox first would close the db, leaving Unmount's Unindex step with no db to use.
	model.Unmount(notebook)
}

// setNotebookCryptoAutoLock sets the idle-minutes threshold for auto-locking encrypted notebooks.
func setNotebookCryptoAutoLock(c *gin.Context) {
	ret := gulu.Ret.NewResult()
	defer c.JSON(http.StatusOK, ret)

	arg, ok := util.JsonArg(c, ret)
	if !ok {
		return
	}

	var autoLockMinutes float64
	if !util.ParseJsonArgs(arg, ret, util.BindJsonArg("autoLockMinutes", &autoLockMinutes, true, false)) {
		return
	}

	minutes := max(int(autoLockMinutes), 0)

	model.SetAutoLockMinutes(minutes)
	model.Conf.Save()
}

// touchEncryptedNotebooks is called by real user interaction on the frontend, or explicitly by a headless client to
// keep the session alive, refreshing the idle timer of unlocked encrypted notebooks.
func touchEncryptedNotebooks(c *gin.Context) {
	model.TouchUnlockedEncryptedBoxes()
	c.JSON(http.StatusOK, gulu.Ret.NewResult())
}

// changeMasterPassword changes the master password for encrypted notebooks.
// After verifying with the old password, it derives a new KEK from the new password and re-encrypts the verifier
// and every encrypted notebook's WrappedDEK.
// It must be called while all encrypted notebooks are locked (no DEK held in memory).
func changeMasterPassword(c *gin.Context) {
	ret := gulu.Ret.NewResult()
	defer c.JSON(http.StatusOK, ret)

	arg, ok := util.JsonArg(c, ret)
	if !ok {
		return
	}

	var oldPassword, newPassword string
	if !util.ParseJsonArgs(arg, ret,
		util.BindJsonArg("oldPassword", &oldPassword, true, true),
		util.BindJsonArg("newPassword", &newPassword, true, true),
	) {
		return
	}

	if err := model.ChangeMasterPassword(oldPassword, newPassword); err != nil {
		ret.Code = -1
		ret.Msg = err.Error()
		return
	}
}

// getEncryptedNotebookStatus returns whether the encrypted notebook feature is enabled and the unlock status of
// each notebook.
func getEncryptedNotebookStatus(c *gin.Context) {
	ret := gulu.Ret.NewResult()
	defer c.JSON(http.StatusOK, ret)

	model.NotebookCryptoMuLock()
	boxIDs := model.ListAllEncryptedBoxIDs()
	enabled := model.NotebookCryptoEnabled()
	model.NotebookCryptoMuUnlock()
	pendingMigration, migrationBoxes := model.MasterPasswordMigrationStatus()
	// Whether a history snapshot of a deleted encrypted notebook exists in the history directory: restoring it
	// depends on the current key backup, so when present the frontend should block the disable entry point
	// (aligned with the backend check in DisableEncryptedNotebook)
	hasHistoryDependency := model.HasEncryptedNotebookHistory()

	boxes := make([]map[string]any, 0, len(boxIDs))
	for _, id := range boxIDs {
		box := model.Conf.Box(id)
		name := ""
		if box != nil {
			name = box.Name
		}
		boxes = append(boxes, map[string]any{
			"id":       id,
			"name":     name,
			"unlocked": model.IsBoxUnlocked(id),
		})
	}

	ret.Data = map[string]any{
		"enabled":              enabled,
		"count":                len(boxIDs),
		"boxes":                boxes,
		"migrationPending":     pendingMigration,
		"migrationBoxes":       migrationBoxes,
		"hasHistoryDependency": hasHistoryDependency,
	}
}

// exportNotebookCryptoBackup exports the key backup file to the export directory for download.
// The backup file does not contain the master password (the salt isn't secret and the verifier is ciphertext); the
// user saves it manually as an independent recovery path separate from sync.
func exportNotebookCryptoBackup(c *gin.Context) {
	ret := gulu.Ret.NewResult()
	defer c.JSON(http.StatusOK, ret)

	downloadPath, err := model.ExportNotebookCryptoBackup()
	if err != nil {
		ret.Code = -1
		ret.Msg = err.Error()
		return
	}
	ret.Data = map[string]any{
		"file": downloadPath,
	}
}

// importNotebookCryptoBackup imports a key backup file to restore the encryption configuration.
// Used to manually restore on a new device or after a reinstall without relying on sync. Rejected if already
// enabled on this machine (to avoid overwriting orphaned data).
func importNotebookCryptoBackup(c *gin.Context) {
	ret := gulu.Ret.NewResult()
	defer c.JSON(http.StatusOK, ret)

	form, err := c.MultipartForm()
	if err != nil {
		ret.Code = -1
		ret.Msg = err.Error()
		return
	}
	if 1 > len(form.File["file"]) {
		ret.Code = -1
		ret.Msg = "file not found"
		return
	}
	fh := form.File["file"][0]
	f, err := fh.Open()
	if err != nil {
		ret.Code = -1
		ret.Msg = err.Error()
		return
	}
	defer f.Close()
	data, err := io.ReadAll(f)
	if err != nil {
		ret.Code = -1
		ret.Msg = err.Error()
		return
	}
	password := ""
	if vals := form.Value["password"]; len(vals) > 0 {
		password = vals[0]
	}
	if err := model.ImportNotebookCryptoBackup(data, password); err != nil {
		ret.Code = -1
		ret.Msg = err.Error()
		return
	}
}
