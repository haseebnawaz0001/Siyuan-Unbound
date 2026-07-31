import {Constants} from "../../constants";
import {Dialog} from "../../dialog";
import {confirmDialog} from "../../dialog/confirmDialog";
import {showMessage} from "../../dialog/message";
import {fetchPost, fetchSyncPost} from "../../util/fetch";
import {isMobile} from "../../util/functions";
import {isEncryptedBox} from "../../util/pathName";
import {saveExportFile} from "../util/compatibility";

// Export options dialog https://github.com/siyuan-note/siyuan/issues/17031
// The common section (8 items) can be reused by other export formats; the Markdown-specific section is only used for Markdown export.
// Default values are always taken from the global window.siyuan.config.export; after confirmation
// they apply only to this export, without modifying the global settings or remembering the last choice.

// Type aliases: constrain field keys to match Config.IExport's field types, avoiding errors from indexing with string
type BoolKey = "addTitle" | "inlineMemo" | "includeSubDocs" | "includeRelatedDocs" | "markdownYFM" | "removeAssetsID";
type IntKey = "blockRefMode" | "blockEmbedMode" | "fileAnnotationRefMode";
type StringKey = "blockRefTextLeft" | "blockRefTextRight" | "tagOpenMarker" | "tagCloseMarker";

interface IExportMdOptions {
    id?: string;
    ids?: string[];
    notebook?: string;
}

// openExportOptionsDialog renders two groups of toggles, "common + Markdown-specific"; on
// confirmation the onConfirm callback passes out all 13 items.
// showSubDocs/showRelatedDocs control whether the "include sub-documents/related documents" items
// are shown (hidden when a single document has no corresponding content).
export const openExportOptionsDialog = (onConfirm: (options: IExportMdOptionsPayload) => void, showSubDocs = true, showRelatedDocs = true) => {
    const conf = window.siyuan.config.export;
    const bool = (id: BoolKey) => `<input id="${id}" class="b3-switch fn__flex-center" type="checkbox" ${conf[id] ? "checked" : ""}>`;
    // Render a select: reuse the settings panel's standard class (fn__flex-center fn__size200); mark
    // selected when the value matches the current global value
    const select = (id: IntKey, options: {value: number; label: string}[]) => {
        const opts = options.map(o =>
            `<option value="${o.value}" ${conf[id] === o.value ? "selected" : ""}>${o.label}</option>`).join("");
        return `<select id="${id}" class="b3-select fn__flex-center fn__size200">${opts}</select>`;
    };
    // One row: title + description on the left, control on the right. Reuses the settings panel's
    // standard layout class (config-item config-wrap)
    const row = (title: string, desc: string, control: string) =>
        `<label class="fn__flex b3-label config-item config-wrap">
            <div class="fn__flex-1">
                <div class="config-name">${title}</div>
                <div class="b3-label__text">${desc}</div>
            </div>
            <span class="fn__space"></span>
            ${control}
        </label>`;
    // Two input boxes side by side, reusing the settings panel's standard width class (fn__size96)
    const textPair = (leftId: StringKey, rightId: StringKey) =>
        `<input id="${leftId}" class="b3-text-field fn__flex-center fn__size96" value="${conf[leftId] ?? ""}">
        <span class="fn__space"></span>
        <input id="${rightId}" class="b3-text-field fn__flex-center fn__size96" value="${conf[rightId] ?? ""}">`;

    const dialog = new Dialog({
        title: window.siyuan.languages.export + " Markdown",
        content: `<div class="b3-dialog__content export-md__content">
    <!-- Common -->
    ${row(window.siyuan.languages.export17, window.siyuan.languages.export18, bool("addTitle"))}
    ${showSubDocs ? row(window.siyuan.languages.includeSubDocs, window.siyuan.languages.includeSubDocsTip, bool("includeSubDocs")) : ""}
    ${showRelatedDocs ? row(window.siyuan.languages.includeRelatedDocs, window.siyuan.languages.includeRelatedDocsTip, bool("includeRelatedDocs")) : ""}
    ${row(window.siyuan.languages.export23, window.siyuan.languages.export24, bool("markdownYFM"))}
    ${row(window.siyuan.languages.removeAssetsID, window.siyuan.languages.removeAssetsIDTip, bool("removeAssetsID"))}
    <!-- Other -->
    ${row(window.siyuan.languages.export31, window.siyuan.languages.export32, bool("inlineMemo"))}
    ${row(window.siyuan.languages.ref, window.siyuan.languages.export11,
        select("blockRefMode", [
            {value: 2, label: window.siyuan.languages.export2},
            {value: 3, label: window.siyuan.languages.export3},
            {value: 4, label: window.siyuan.languages.export4},
        ]))}
    ${row(window.siyuan.languages.blockEmbed, window.siyuan.languages.export12,
        select("blockEmbedMode", [
            {value: 0, label: window.siyuan.languages.export0},
            {value: 1, label: window.siyuan.languages.export1},
        ]))}
    ${row(window.siyuan.languages.export5, window.siyuan.languages.export6,
        select("fileAnnotationRefMode", [
            {value: 0, label: window.siyuan.languages.export7},
            {value: 1, label: window.siyuan.languages.export8},
        ]))}
    ${row(window.siyuan.languages.export13, window.siyuan.languages.export14,
        textPair("blockRefTextLeft", "blockRefTextRight"))}
    ${row(window.siyuan.languages.export15, window.siyuan.languages.export16,
        textPair("tagOpenMarker", "tagCloseMarker"))}
</div>
<div class="b3-dialog__action">
    <button class="b3-button b3-button--cancel">${window.siyuan.languages.cancel}</button><div class="fn__space"></div>
    <button class="b3-button b3-button--text">${window.siyuan.languages.confirm}</button>
</div>`,
        width: "520px",
        height: isMobile() ? "70vh" : "60vh",
    });
    dialog.element.setAttribute("data-key", Constants.DIALOG_EXPORTMARKDOWN);

    const el = dialog.element;
    // The control may not be rendered (e.g. when there are no sub-documents); fall back to the global config value in that case
    const boolVal = (id: BoolKey) => {
        const input = el.querySelector("#" + id) as HTMLInputElement;
        return input ? input.checked : conf[id];
    };
    const collect = (): IExportMdOptionsPayload => ({
        addTitle: (el.querySelector("#addTitle") as HTMLInputElement).checked,
        inlineMemo: (el.querySelector("#inlineMemo") as HTMLInputElement).checked,
        blockRefMode: parseInt((el.querySelector("#blockRefMode") as HTMLSelectElement).value, 10),
        blockEmbedMode: parseInt((el.querySelector("#blockEmbedMode") as HTMLSelectElement).value, 10),
        fileAnnotationRefMode: parseInt((el.querySelector("#fileAnnotationRefMode") as HTMLSelectElement).value, 10),
        blockRefTextLeft: (el.querySelector("#blockRefTextLeft") as HTMLInputElement).value,
        blockRefTextRight: (el.querySelector("#blockRefTextRight") as HTMLInputElement).value,
        tagOpenMarker: (el.querySelector("#tagOpenMarker") as HTMLInputElement).value,
        tagCloseMarker: (el.querySelector("#tagCloseMarker") as HTMLInputElement).value,
        includeSubDocs: boolVal("includeSubDocs"),
        includeRelatedDocs: boolVal("includeRelatedDocs"),
        markdownYFM: (el.querySelector("#markdownYFM") as HTMLInputElement).checked,
        removeAssetsID: (el.querySelector("#removeAssetsID") as HTMLInputElement).checked,
    });

    const btnsElement = el.querySelectorAll(".b3-button");
    btnsElement[0].addEventListener("click", () => {
        dialog.destroy();
    });
    btnsElement[1].addEventListener("click", () => {
        const payload = collect();
        dialog.destroy();
        onConfirm(payload);
    });
};

interface IExportMdOptionsPayload {
    addTitle: boolean;
    inlineMemo: boolean;
    blockRefMode: number;
    blockEmbedMode: number;
    fileAnnotationRefMode: number;
    blockRefTextLeft: string;
    blockRefTextRight: string;
    tagOpenMarker: string;
    tagCloseMarker: string;
    includeSubDocs: boolean;
    includeRelatedDocs: boolean;
    markdownYFM: boolean;
    removeAssetsID: boolean;
}

// exportMarkdownZip is the entry point for Markdown .zip export: shows the options dialog, and after
// confirmation calls the corresponding API based on id/ids/notebook.
// For a single document, document info is queried first, and the corresponding config items are
// hidden if there are no sub-documents/related documents, to reduce clutter.
export const exportMarkdownZip = async(options: IExportMdOptions) => {
    let showSubDocs = true;
    let showRelatedDocs = true;
    let encrypted = false;
    if (options.id) {
        // Query whether the document has sub-documents, references, and bound databases; hide the corresponding config items if not #17031
        const docInfo = await fetchSyncPost("/api/block/getDocInfo", {id: options.id});
        const data = docInfo.data;
        showSubDocs = 0 < data.subFileCount;
        showRelatedDocs = 0 < (data.refCount || 0) || 0 < (data.attrViews?.length || 0);
        const blockInfo = await fetchSyncPost("/api/block/getBlockInfo", {id: options.id});
        encrypted = blockInfo.code === 0 && isEncryptedBox(blockInfo.data.box);
    }
    openExportOptionsDialog(params => {
        const exportMarkdown = () => {
        const msgId = showMessage(window.siyuan.languages.exporting, -1);
        const cb = (response: IWebSocketData) => saveExportFile(response.data.zip, msgId);
        if (options.id) {
            fetchPost("/api/export/exportMd", {id: options.id, ...params}, cb);
        } else if (options.ids) {
            fetchPost("/api/export/exportMds", {ids: options.ids, ...params}, cb);
        } else {
            fetchPost("/api/export/exportNotebookMd", {notebook: options.notebook, ...params}, cb);
        }
        };
        if (encrypted) {
            confirmDialog("⚠️ " + window.siyuan.languages.export, window.siyuan.languages.encryptedExportRiskTip, exportMarkdown);
            return;
        }
        exportMarkdown();
    }, showSubDocs, showRelatedDocs);
};
