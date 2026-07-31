import {escapeHtml} from "./escape";
import {Constants} from "../constants";
import {pathPosix} from "./pathName";

export const getWorkspaceName = () => {
    const dir = window.siyuan.config.system.workspaceDir;
    // In a browser environment the kernel does not return the workspace's absolute path, so fall
    // back to the "Workspace" label.
    // siyuanNote cannot be used here: setTitle's title template already includes siyuanNote, which
    // would duplicate it as "SiYuan Note - SiYuan Note".
    // Note: this function may be called before languages has loaded (e.g. from setBodyHighlight),
    // hence the optional chaining, in which case it returns undefined, and the caller
    // (setBodyHighlight's if(!name) return) skips further handling
    // https://github.com/siyuan-note/siyuan/issues/17410
    return dir ? pathPosix().basename(dir.replace(/\\/g, "/")) : window.siyuan.languages?.workspace;
};

export const setTitle = (title: string, showVersionTitle = false) => {
    const dragElement = document.getElementById("drag");
    const workspaceName = getWorkspaceName();
    if (showVersionTitle) {
        const versionTitle = `${workspaceName} - ${window.siyuan.languages.siyuanNote} v${Constants.SIYUAN_VERSION}`;
        document.title = versionTitle;
        if (!window.siyuan.config.appearance.hideToolbar && dragElement) {
            dragElement.textContent = versionTitle;
            dragElement.setAttribute("title", versionTitle);
        }
    } else {
        title = title.trim() || window.siyuan.languages["_kernel"][16];
        document.title = `${title} - ${workspaceName} - ${window.siyuan.languages.siyuanNote} v${Constants.SIYUAN_VERSION}`;
        if (!window.siyuan.config.appearance.hideToolbar && dragElement) {
            dragElement.setAttribute("title", title);
            dragElement.innerHTML = escapeHtml(title);
        }
    }
};
