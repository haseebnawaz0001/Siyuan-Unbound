import {Constants} from "../../constants";
import {fetchPost} from "../../util/fetch";
import {confirmDialog} from "../../dialog/confirmDialog";
import {showMessage} from "../../dialog/message";
import {waitForPendingTransactions} from "../util/transactionQueue";
/// #if !MOBILE
import {getActiveTab} from "../../layout/tabUtil";
/// #endif
/// #if MOBILE
import {getCurrentEditor} from "../../mobile/editor";
/// #endif

// Local mirror: caches {canUndo, canRedo} per rootID, so button state can be read with zero fetches.
// Updated on edits (the add landing point), undo/redo responses, and WS broadcasts (context.undoState).
interface IUndoStateMirror {
    canUndo: boolean;
    canRedo: boolean;
}

const undoStateMirror = new Map<string, IUndoStateMirror>();
let isUndoing = false; // Prevent re-entry: ignore subsequent triggers while an undo/redo is in progress

export const markMirror = (rootID: string, state: Partial<IUndoStateMirror>) => {
    const cur = undoStateMirror.get(rootID) || {canUndo: false, canRedo: false};
    undoStateMirror.set(rootID, {...cur, ...state});
};

export const getMirror = (rootID: string): IUndoStateMirror => {
    return undoStateMirror.get(rootID) || {canUndo: false, canRedo: false};
};

// Batch-updates the mirror from a WS broadcast's context.undoState (multi-window/multi-device sync)
export const syncMirrorFromBroadcast = (undoState: { [rootID: string]: { canUndo: boolean; canRedo: boolean } }) => {
    if (!undoState) {
        return;
    }
    Object.entries(undoState).forEach(([rootID, state]) => {
        undoStateMirror.set(rootID, {canUndo: !!state.canUndo, canRedo: !!state.canRedo});
    });
};

// Proactively initializes the mirror when a document is opened (low-frequency, not on the selectionchange hot path)
export const initMirror = (rootID: string) => {
    if (!rootID) {
        return;
    }
    fetchPost("/api/transactions/undoState", {rootID}, (response) => {
        const data = response.data;
        if (data) {
            undoStateMirror.set(rootID, {canUndo: !!data.canUndo, canRedo: !!data.canRedo});
        }
    });
};

// Refreshes the undo/redo button state for the given protyle (reads the mirror, zero fetches)
export const refreshUndoButtons = (protyle: IProtyle) => {
    if (!protyle.block?.rootID) {
        return;
    }
    const state = getMirror(protyle.block.rootID);
    if (protyle.breadcrumb) {
        const parent = protyle.breadcrumb.element.parentElement;
        const undoElement = parent.querySelector('[data-type="undo"]') as HTMLElement;
        const redoElement = parent.querySelector('[data-type="redo"]') as HTMLElement;
        if (undoElement) {
            if (state.canUndo) {
                undoElement.removeAttribute("disabled");
            } else {
                undoElement.setAttribute("disabled", "disabled");
            }
        }
        if (redoElement) {
            if (state.canRedo) {
                redoElement.removeAttribute("disabled");
            } else {
                redoElement.setAttribute("disabled", "disabled");
            }
        }
    }
};

export const getActiveProtyle = (): IProtyle => {
    /// #if MOBILE
    const editor = getCurrentEditor();
    return editor?.protyle;
    /// #else
    const activeTab = getActiveTab();
    const model = activeTab?.model;
    if (model && (model as any).editor?.protyle) {
        return (model as any).editor.protyle;
    }
    // Fallback: the one focused in a search/backlink/custom editor
    /// #if !MOBILE
    const allProtyle = (window as any).siyuan?.blockPanels || [];
    for (const panel of allProtyle) {
        if (panel.element && document.activeElement && panel.element.contains(document.activeElement)) {
            return panel.editor?.protyle;
        }
    }
    /// #endif
    return undefined;
    /// #endif
};

// Resolves a list of rootIDs to document names, used for the cross-document undo confirmation prompt
const resolveRootNames = async (rootIDs: string[]): Promise<string[]> => {
    const names: string[] = [];
    for (const id of rootIDs) {
        await new Promise<void>((resolve) => {
            fetchPost("/api/filetree/getHPathByID", {id}, (response: IWebSocketData) => {
                if (response.code === 0 && response.data) {
                    names.push(response.data as string);
                } else {
                    names.push(id);
                }
                resolve();
            });
        });
    }
    return names;
};

const focusRootIDs = (rootIDs: string[], focusBlockId?: string) => {
    // Only scroll the initiating window's focused protyle to the changed block; other documents are
    // not forced to reopen (the physical result of the undo lives in the initiating document)
    const protyle = getActiveProtyle();
    if (protyle && rootIDs.includes(protyle.block?.rootID) && focusBlockId) {
        const target = protyle.wysiwyg.element.querySelector(`[data-node-id="${focusBlockId}"]`);
        if (target) {
            const rect = target.getBoundingClientRect();
            // Only scroll when the changed block is outside the viewport, to avoid disrupting the
            // user's current scroll position
            if (rect.bottom < 0 || rect.top > window.innerHeight) {
                target.scrollIntoView({behavior: "smooth", block: "center"});
            }
        }
    }
};

// Request undo: read the mirror to check if undo is possible -> cross-document prompt -> call kernel
// undo -> local optimistic apply + update the mirror
export const requestUndo = async (protyle: IProtyle) => {
    if (!protyle || isUndoing) {
        return;
    }
    const rootID = protyle.block?.rootID;
    if (!rootID) {
        return;
    }

    const state = getMirror(rootID);
    if (!state.canUndo) {
        return; // Semantics B: do nothing when the stack is empty
    }

    // Set the lock as early as possible, to prevent a new undo/redo from being triggered while the
    // confirmation dialog is showing (including the peek and confirmation phases)
    isUndoing = true;
    await waitForPendingTransactions(protyle);

    // Cross-document prompt (standard #1): first peek at the mutatedRootIDs on top of the stack
    let peekMutatedRootIDs: string[] = [];
    await new Promise<void>((resolve) => {
        fetchPost("/api/transactions/undoState", {rootID}, (response) => {
            if (response.data?.peekMutatedRootIDs) {
                peekMutatedRootIDs = response.data.peekMutatedRootIDs;
            }
            resolve();
        });
    });

    if (peekMutatedRootIDs.length > 1) {
        const names = await resolveRootNames(peekMutatedRootIDs);
        // Intercept the current editor's keyboard input during confirmation (the mask only blocks
        // mouse clicks, not keyboard event bubbling)
        const blockInput = (e: Event) => {
            e.stopImmediatePropagation();
            e.preventDefault();
        };
        protyle.wysiwyg.element.addEventListener("keydown", blockInput, true);
        protyle.wysiwyg.element.addEventListener("beforeinput", blockInput, true);
        const confirmed = await new Promise<boolean>((resolve) => {
            confirmDialog(`⚠️ ${window.siyuan.languages.undo}`,
                `${window.siyuan.languages.undoCrossDocConfirm}<div style="margin-top: 8px;">${names.map(n => `• ${n}`).join("<br>")}</div>`,
                () => resolve(true),
                () => resolve(false));
        });
        protyle.wysiwyg.element.removeEventListener("keydown", blockInput, true);
        protyle.wysiwyg.element.removeEventListener("beforeinput", blockInput, true);
        if (!confirmed) {
            isUndoing = false; // Rejected: reset the lock, the stack and mirror remain unchanged
            return;
        }
    }

    fetchPost("/api/transactions/undo", {
        rootID,
        app: Constants.SIYUAN_APPID,
        session: protyle.id,
    }, (response) => {
        isUndoing = false;
        const data = response.data;
        if (!data) {
            return;
        }
        if (data.failed) {
            // Undo execution failed: the kernel has already un-popped the stack, the mirror remains unchanged, notify the user
            if (data.msg) {
                showMessage(data.msg);
            }
            return;
        }
        if (!data.undoOperations || data.undoOperations.length === 0) {
            // The stack is empty or there's nothing to undo
            markMirror(rootID, {canUndo: !!data.canUndo, canRedo: !!data.canRedo});
            refreshUndoButtons(protyle);
            return;
        }
        markMirror(rootID, {canUndo: !!data.canUndo, canRedo: !!data.canRedo});
        const mutatedRootIDs: string[] = data.mutatedRootIDs || [];
        if (mutatedRootIDs.length > 1) {
            // Cross-document undo: doOperations' anchors are spread across multiple documents, so the
            // current protyle cannot apply them locally and optimistically.
            // Instead, rely on the kernel broadcast (including to the initiator) to refresh the DOM of
            // all involved documents.
            // renderLocal is not called here, to avoid applying a cross-document move on the wrong
            // protyle, which would cause a frontend/backend mismatch.
            refreshUndoButtons(protyle);
            // The broadcast will reach the current window (/undo uses PushModeBroadcast for
            // cross-document cases), triggering onTransaction to refresh the DOM
        } else {
            // Single-document undo: the initiating window applies doOperations locally and
            // optimistically (the operations the kernel actually executed, e.g. insert to restore a block)
            protyle.undo.renderLocal(protyle, data.doOperations);
            refreshUndoButtons(protyle);
            const focusBlockId = data.doOperations?.find((op: IOperation) => op.id)?.id;
            focusRootIDs(mutatedRootIDs, focusBlockId);
        }
    });
};

// Request redo: symmetrical to undo; redo does not prompt (its inverse was already confirmed during the undo)
export const requestRedo = async (protyle: IProtyle) => {
    if (!protyle || isUndoing) {
        return;
    }
    const rootID = protyle.block?.rootID;
    if (!rootID) {
        return;
    }

    const state = getMirror(rootID);
    if (!state.canRedo) {
        return;
    }

    isUndoing = true;
    await waitForPendingTransactions(protyle);
    fetchPost("/api/transactions/redo", {
        rootID,
        app: Constants.SIYUAN_APPID,
        session: protyle.id,
    }, (response) => {
        isUndoing = false;
        const data = response.data;
        if (!data) {
            return;
        }
        if (data.failed) {
            // Redo execution failed: the kernel has already rolled back the stack, the mirror remains unchanged, notify the user
            if (data.msg) {
                showMessage(data.msg);
            }
            return;
        }
        if (!data.doOperations || data.doOperations.length === 0) {
            markMirror(rootID, {canUndo: !!data.canUndo, canRedo: !!data.canRedo});
            refreshUndoButtons(protyle);
            return;
        }
        markMirror(rootID, {canUndo: !!data.canUndo, canRedo: !!data.canRedo});
        const mutatedRootIDs: string[] = data.mutatedRootIDs || [];
        if (mutatedRootIDs.length > 1) {
            // Cross-document redo: anchors are spread across multiple documents, rely on the kernel
            // broadcast (including to the initiator) to refresh
            refreshUndoButtons(protyle);
        } else {
            protyle.undo.renderLocal(protyle, data.doOperations);
            refreshUndoButtons(protyle);
            const focusBlockId = data.doOperations?.find((op: IOperation) => op.id)?.id;
            focusRootIDs(mutatedRootIDs, focusBlockId);
        }
    });
};
