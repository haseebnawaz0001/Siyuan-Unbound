import {isLocalPath, pathPosix} from "../util/pathName";
/// #if !BROWSER
import {shell} from "electron";
/// #endif
import {getSearch} from "../util/functions";
import {Constants} from "../constants";
import {processSiYuanUri} from "../util/uri";
/// #if !MOBILE
import {openAsset, openBy} from "./util";
/// #endif
import {showMessage} from "../dialog/message";
import {isInIOS, isInAndroid, isInHarmony} from "../protyle/util/compatibility";
import type {App} from "../index";

export const openLink = (app: App, aLink: string, event?: MouseEvent, ctrlIsPressed = false) => {
    let linkAddress = Lute.UnEscapeHTMLStr(aLink);
    let pdfParams;
    if (isLocalPath(linkAddress) && !linkAddress.startsWith("file://") && linkAddress.indexOf(".pdf") > -1) {
        const pdfAddress = linkAddress.split("/");
        if (pdfAddress.length === 3 && pdfAddress[0] === "assets" && pdfAddress[1].endsWith(".pdf") && /\d{14}-\w{7}/.test(pdfAddress[2])) {
            linkAddress = `assets/${pdfAddress[1]}`;
            pdfParams = pdfAddress[2];
        } else {
            pdfParams = parseInt(getSearch("page", linkAddress));
            linkAddress = linkAddress.split("?page")[0];
        }
    }
    if (processSiYuanUri(app, linkAddress)) {
        return;
    }
    /// #if MOBILE
    openByMobile(linkAddress);
    /// #else
    if (isLocalPath(linkAddress)) {
        if (Constants.SIYUAN_ASSETS_EXTS.includes(pathPosix().extname(linkAddress)) &&
            (
                !linkAddress.endsWith(".pdf") ||
                // For a local PDF, only open it with SiYuan if the path starts with assets/
                (linkAddress.endsWith(".pdf") && linkAddress.startsWith("assets/"))
            )
        ) {
            if (event && event.altKey) {
                openAsset(app, linkAddress, pdfParams);
            } else if (event && event.shiftKey) {
                /// #if !BROWSER
                openBy(linkAddress, "app");
                /// #else
                openByMobile(linkAddress);
                /// #endif
            } else if (ctrlIsPressed) {
                /// #if !BROWSER
                openBy(linkAddress, "folder");
                /// #else
                openByMobile(linkAddress);
                /// #endif
            } else {
                openAsset(app, linkAddress, pdfParams, !window.siyuan.config.fileTree.noSplitScreenWhenOpenTab ? "right" : null);
            }
        } else {
            /// #if !BROWSER
            if (ctrlIsPressed) {
                openBy(linkAddress, "folder");
            } else {
                openBy(linkAddress, "app");
            }
            /// #else
            openByMobile(linkAddress);
            /// #endif
        }
    } else if (linkAddress) {
        if (0 > linkAddress.indexOf(":")) {
            // Check for ":" instead of "://", since "Open external application protocol invalid" https://github.com/siyuan-note/siyuan/issues/10075
            // Support click to open hyperlinks like `www.foo.com` https://github.com/siyuan-note/siyuan/issues/9986
            linkAddress = `https://${linkAddress}`;
        }
        /// #if !BROWSER
        shell.openExternal(linkAddress).catch((e) => {
            showMessage(e);
        });
        /// #else
        openByMobile(linkAddress);
        /// #endif
    }
    /// #endif
};

export const openByMobile = (uri: string) => {
    if (!uri) {
        return;
    }
    if (processSiYuanUri(window.siyuan.ws.app, uri)) {
        return;
    }
    if (isInIOS()) {
        if (uri.startsWith("assets/")) {
            // On versions before iOS 16.7, the uri needs encodeURIComponent
            // Keep the query parameters (e.g. ?box=<id>) as-is, only encode the path part
            const pathAndQuery = uri.replace("assets/", "");
            const queryIdx = pathAndQuery.indexOf("?");
            let encodedPath = pathAndQuery;
            let query = "";
            if (queryIdx >= 0) {
                encodedPath = pathAndQuery.substring(0, queryIdx);
                query = pathAndQuery.substring(queryIdx);
            }
            window.webkit.messageHandlers.openLink.postMessage(location.origin + "/assets/" + encodeURIComponent(encodedPath) + query);
        } else if (uri.startsWith("/")) {
            // What the export-zip endpoint returns is already encoded, so it must not be encoded again
            window.webkit.messageHandlers.openLink.postMessage(location.origin + uri);
        } else {
            try {
                new URL(uri);
                window.webkit.messageHandlers.openLink.postMessage(uri);
            } catch (e) {
                window.webkit.messageHandlers.openLink.postMessage("https://" + uri);
            }
        }
    } else if (isInAndroid()) {
        window.JSAndroid.openExternal(uri);
    } else if (isInHarmony()) {
        window.JSHarmony.openExternal(uri);
    } else {
        window.open(uri);
    }
};
