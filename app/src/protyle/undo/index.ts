import {onTransaction, transaction} from "../wysiwyg/transaction";
import {preventScroll} from "../scroll/preventScroll";
import {Constants} from "../../constants";
import {hideElements} from "../ui/hideElements";
import {matchHotKey} from "../util/hotKey";
import {restoreUndoFocus} from "../util/selection";
import {ipcRenderer} from "electron";
import {markMirror, refreshUndoButtons, requestRedo, requestUndo} from "./globalUndo";
import {scrollCenter} from "../../util/highlightById";

// Unified undo/redo contract: kernel mode is implemented by Undo (forwards to the kernel); lite mode
// is implemented by LocalUndo (a frontend operation log).
export interface IUndo {
    undo(protyle: IProtyle): void;

    redo(protyle: IProtyle): void;

    add(doOperations: IOperation[], undoOperations: IOperation[], protyle: IProtyle): void;

    clear(): void;

    // Unique to kernel mode: the initiating window applies the operation locally and optimistically
    // (not needed by lite mode's LocalUndo).
    renderLocal?(protyle: IProtyle, operations: IOperation[]): void;
}

interface IOperations {
    doOperations: IOperation[];
    undoOperations: IOperation[];
}

const syncToolbarRange = (protyle: IProtyle) => {
    if (getSelection().rangeCount > 0) {
        protyle.toolbar.range = getSelection().getRangeAt(0);
    }
};

// The authoritative undo stack has moved down into the kernel (GlobalUndoLog), shared by the
// frontend per rootID.
// This class only retains the render logic for the initiating window's local optimistic apply
// (renderLocal, taking the isUndo=true branch to keep cursor restoration/fold/zoom fallbacks), plus
// button state refresh.
export class Undo implements IUndo {
    public undo(protyle: IProtyle) {
        if (protyle.disabled) {
            return;
        }
        protyle.wysiwyg.flushPendingInput();
        // Forward to the global Manager: the kernel pops the stack and broadcasts, and the
        // initiating window applies it locally and optimistically
        requestUndo(protyle);
    }

    public redo(protyle: IProtyle) {
        if (protyle.disabled) {
            return;
        }
        protyle.wysiwyg.flushPendingInput();
        requestRedo(protyle);
    }

    // renderLocal only applies the operation locally on the initiating window (isUndo=true); it does
    // not POST to the kernel (the kernel's undo/redo endpoint has already executed the transaction
    // and broadcast it). Preserves the cursor restoration/fold/zoom/lastHTMLs behavior.
    public renderLocal(protyle: IProtyle, operations: IOperation[]) {
        hideElements(["hint", "gutter"], protyle);
        protyle.wysiwyg.lastHTMLs = {};
        for (let i = operations.length - 1; i >= 0; i--) {
            if (operations[i].action === "insert") {
                if (operations[i].context) {
                    operations[i].context.setRange = "true";
                } else {
                    operations[i].context = {setRange: "true"};

                }
                break;
            }
        }
        onTransaction(protyle, operations, true);
        if (restoreUndoFocus(protyle, operations)) {
            scrollCenter(protyle);
        }
        document.querySelector(".av__panel")?.remove();
        preventScroll(protyle);
        // Sync the toolbar range, to avoid it becoming detached after undo/redo replaces the DOM,
        // which would otherwise cause a subsequent async operation (e.g. F3 creating a sub-document)
        // to read an invalid range and throw https://github.com/siyuan-note/siyuan/issues/17896
        syncToolbarRange(protyle);
    }

    // add is downgraded to: not pushing to a stack (the kernel already Records after commit), only
    // updating the local mirror + refreshing button state.
    // The signature is kept for compatibility with transaction.ts's call sites.
    public add(doOperations: IOperation[], undoOperations: IOperation[], protyle: IProtyle) {
        if (protyle.block?.rootID) {
            markMirror(protyle.block.rootID, {canUndo: true, canRedo: false});
        }
        refreshUndoButtons(protyle);
    }

    public clear() {
        // The kernel's global stack is not cleared when the frontend editor is destroyed/reloaded
        // (it's shared across windows).
        // Locally, only refresh the button state; the mirror entry is kept for calibration on reopen.
    }
}

// Lite mode's frontend undo: nothing is persisted, there's no rootID, and the kernel's
// GlobalUndoLog cannot be used.
// So undo/redo is maintained on the frontend using an IOperation log. During replay,
// onTransaction(ops, true) applies it to the DOM locally.
export class LocalUndo implements IUndo {
    private hasUndo = false;
    public redoStack: IOperations[];
    public undoStack: IOperations[];

    constructor() {
        this.redoStack = [];
        this.undoStack = [];
    }

    public undo(protyle: IProtyle) {
        if (protyle.disabled) {
            return;
        }
        protyle.wysiwyg.flushPendingInput();
        if (this.undoStack.length === 0) {
            return;
        }
        const state = this.undoStack.pop();
        this.render(protyle, state, false);
        this.hasUndo = true;
        this.redoStack.push(state);
        if (protyle.breadcrumb) {
            const undoElement = protyle.breadcrumb.element.parentElement.querySelector('[data-type="undo"]');
            if (undoElement) {
                if (this.undoStack.length === 0) {
                    undoElement.setAttribute("disabled", "true");
                }
                protyle.breadcrumb.element.parentElement.querySelector('[data-type="redo"]').removeAttribute("disabled");
            }
        }
    }

    public redo(protyle: IProtyle) {
        if (protyle.disabled) {
            return;
        }
        protyle.wysiwyg.flushPendingInput();
        if (this.redoStack.length === 0) {
            return;
        }
        const state = this.redoStack.pop();
        this.render(protyle, state, true);
        this.undoStack.push(state);
        if (protyle.breadcrumb) {
            const redoElement = protyle.breadcrumb.element.parentElement.querySelector('[data-type="redo"]');
            if (redoElement) {
                protyle.breadcrumb.element.parentElement.querySelector('[data-type="undo"]').removeAttribute("disabled");
                if (this.redoStack.length === 0) {
                    redoElement.setAttribute("disabled", "true");
                }
            }
        }
    }

    private render(protyle: IProtyle, state: IOperations, redo: boolean) {
        hideElements(["hint", "gutter"], protyle);
        protyle.wysiwyg.lastHTMLs = {};
        if (!redo) {
            for (let i = state.undoOperations.length - 1; i >= 0; i--) {
                if (state.undoOperations[i].action === "insert") {
                    if (state.undoOperations[i].context) {
                        state.undoOperations[i].context.setRange = "true";
                    } else {
                        state.undoOperations[i].context = {setRange: "true"};
                    }
                    break;
                }
            }
            onTransaction(protyle, state.undoOperations, true);
            transaction(protyle, state.undoOperations, undefined, {skipSync: true});
            restoreUndoFocus(protyle, state.undoOperations);
        } else {
            for (let i = state.doOperations.length - 1; i >= 0; i--) {
                if (state.doOperations[i].action === "insert") {
                    if (state.doOperations[i].context) {
                        state.doOperations[i].context.setRange = "true";
                    } else {
                        state.doOperations[i].context = {setRange: "true"};
                    }
                    break;
                }
            }
            onTransaction(protyle, state.doOperations, true);
            transaction(protyle, state.doOperations, undefined, {skipSync: true});
        }
        syncToolbarRange(protyle);
        document.querySelector(".av__panel")?.remove();
        preventScroll(protyle);
        scrollCenter(protyle);
    }

    public replace(doOperations: IOperation[], protyle: IProtyle) {
        // A replace triggered by undo causes a stack error https://github.com/siyuan-note/siyuan/issues/9178
        if (this.hasUndo && this.redoStack.length > 0) {
            this.undoStack.push(this.redoStack.pop());
            this.redoStack = [];
            this.hasUndo = false;
            if (protyle.breadcrumb) {
                const redoElement = protyle.breadcrumb.element.parentElement.querySelector('[data-type="redo"]');
                if (redoElement) {
                    redoElement.setAttribute("disabled", "true");
                }
            }
        }
        if (this.undoStack.length > 0) {
            this.undoStack[this.undoStack.length - 1].doOperations = doOperations;
        }
    }

    public add(doOperations: IOperation[], undoOperations: IOperation[], protyle: IProtyle) {
        this.undoStack.push({undoOperations, doOperations});
        if (this.undoStack.length > Constants.SIZE_UNDO) {
            this.undoStack.shift();
        }
        if (this.hasUndo) {
            this.redoStack = [];
            this.hasUndo = false;
        }
        if (protyle.breadcrumb) {
            const undoElement = protyle.breadcrumb.element.parentElement.querySelector('[data-type="undo"]');
            if (undoElement) {
                undoElement.removeAttribute("disabled");
            }
        }
    }

    public clear() {
        this.undoStack = [];
        this.redoStack = [];
    }
}

export const electronUndo = (event: KeyboardEvent) => {
    /// #if !BROWSER
    if (matchHotKey(window.siyuan.config.keymap.editor.general.undo.custom, event)) {
        ipcRenderer.send(Constants.SIYUAN_CMD, "undo");
        event.preventDefault();
        event.stopPropagation();
        return true;
    }
    if (matchHotKey(window.siyuan.config.keymap.editor.general.redo.custom, event)) {
        ipcRenderer.send(Constants.SIYUAN_CMD, "redo");
        event.preventDefault();
        event.stopPropagation();
        return true;
    }
    /// #endif
    return false;
};
