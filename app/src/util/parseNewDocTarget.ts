/**
 * Resolves the path for a new document. The input is the path template as rendered by the kernel.
 *
 * Callers first collect `path`, then convert it to `hPath` before passing it in; when crossing
 * notebooks, `hPath` is fixed to `/`.
 *
 * Title priority: `name` > the last path segment (document-name mode) > empty. `name` replaces
 * the last segment rather than being appended to it.
 *
 * Three shapes:
 * - Trailing `/` -> parent document path: the path is a chain of parent documents, and the new
 *   document is created inside the innermost parent; the title is empty when `name` is not passed
 * - No trailing `/` -> document name: the last segment is the title, the rest is the parent path
 * - Empty -> same notebook + has context means a subdocument of the current document; crossing
 *   notebooks or no context means the target notebook's root path
 *
 * A leading `/` resolves from the root, otherwise it is relative to `hPath`; `..` goes up one
 * level, stopping at `/` if already at the root.
 * When crossing notebooks, a relative path is prefixed with `/` and resolved against the target
 * notebook's root; an empty path also falls back to the target notebook's root path.
 */

import {mergePathSegments} from "./mergePathSegments";

/** The kernel's `createDocsByHPath` creates documents level by level according to the HPath */
export type NewDocTargetByHPath = {
    kind: "hPath";
    targetNotebookId: string;
    hPath: string;
    title: string;
};

/** Creates a subdocument under a known parent path */
export type NewDocTargetSubDoc = {
    kind: "subDoc";
    targetNotebookId: string;
    parentPath: string;
    title: string;
};

export type NewDocTarget = NewDocTargetByHPath | NewDocTargetSubDoc;

/** Resolves the new-document target from the save-path configuration */
export const getNewDocTargetFromSavePath = (request: {
    templatePath: string;
    hPath: string;
    targetNotebookId: string;
    currentNotebookId: string;
    name?: string;
    hasFocusTarget: boolean;
    currentPath?: string;
}): NewDocTarget => {
    const {targetNotebookId} = request;

    let templatePath = request.templatePath.trim();
    let isAbsolute = templatePath.startsWith("/");
    if (targetNotebookId !== request.currentNotebookId && templatePath && !isAbsolute) {
        // A relative path has no anchor when crossing notebooks, so prefix it with `/` to resolve
        // it against the target notebook's root path
        templatePath = "/" + templatePath;
        isAbsolute = true;
    }

    // Empty path + same notebook + has context + no name: create an empty-title subdocument under
    // the known parent path.
    // When crossing notebooks, currentPath belongs to the current notebook and is invalid in the
    // target notebook, so it falls through to the hPath logic below, which falls back to the
    // target notebook's root
    if (!templatePath && request.hasFocusTarget && !request.name
        && targetNotebookId === request.currentNotebookId) {
        return {
            kind: "subDoc",
            targetNotebookId,
            parentPath: request.currentPath || "/",
            title: "",
        };
    }

    let parentTemplate: string;
    let title = "";
    if (!templatePath) {
        // Empty path + has context -> under the current path; empty path + no context -> the notebook root
        parentTemplate = request.hasFocusTarget ? "" : "/";
        title = request.name || "";
    } else if (templatePath.endsWith("/")) {
        // Has a trailing `/`: resolve the chain of parent documents, and create the new one inside the innermost parent
        parentTemplate = templatePath;
        title = request.name || "";
    } else {
        const segments = templatePath.split("/").filter(Boolean);
        if (segments.length <= 1) {
            // Document name
            parentTemplate = isAbsolute ? "/" : "";
        } else {
            // Path: drop the last segment (the document name); the remaining segments form the parent path template
            const parentSegmentPath = segments.slice(0, -1).join("/");
            parentTemplate = isAbsolute ? "/" + parentSegmentPath : parentSegmentPath;
        }
        title = request.name || segments[segments.length - 1];
    }

    // Merge the path template into hPath
    const templateSegments = parentTemplate.split("/").filter(Boolean);
    let parentPathSegments: string[];
    if (parentTemplate.startsWith("/")) {
        // Absolute path: computed from the notebook root
        parentPathSegments = mergePathSegments([], templateSegments);
    } else {
        // Relative path: computed from the current hPath
        parentPathSegments = mergePathSegments(request.hPath.split("/").filter(Boolean), templateSegments);
    }

    let hPath: string;
    if (title) {
        hPath = "/" + [...parentPathSegments, title].join("/");
    } else {
        // With an empty title, keep the trailing `/` so hPath's last segment is empty; the kernel
        // then creates the new subdocument inside the parent document chain
        hPath = parentPathSegments.length === 0 ? "/" : "/" + parentPathSegments.join("/") + "/";
    }

    return {
        kind: "hPath",
        targetNotebookId,
        hPath,
        title,
    };
};

/**
 * A specific location in the file tree: creates a subdocument under `currentPath`, with the same
 * title rules as the save path
 */
export const getNewDocTargetFromTree = (request: {
    templatePath: string;
    currentNotebookId: string;
    currentPath: string;
    name?: string;
}): NewDocTargetSubDoc => {
    const templatePath = request.templatePath.trim();
    let title = "";
    if (request.name) {
        title = request.name;
    } else if (templatePath && !templatePath.endsWith("/")) {
        const segments = templatePath.split("/").filter(Boolean);
        title = segments[segments.length - 1];
    }
    return {
        kind: "subDoc",
        targetNotebookId: request.currentNotebookId,
        parentPath: request.currentPath || "/",
        title,
    };
};
