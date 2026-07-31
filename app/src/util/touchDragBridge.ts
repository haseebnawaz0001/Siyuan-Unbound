import {stopScrollAnimation} from "../boot/globalEvent/dragover";
import {Constants} from "../constants";

// Shared long-press gate state: sliding shortly after a touch counts as scrolling and yields to
// native scroll; only sliding after the long press has settled enters drag mode
interface LongPressGate {
    startX: number;
    startY: number;
    touchStartTime: number;
    requireLongPress: boolean;
    longPressCancelled: boolean;
    // The input source is a mouse: some tablet WebViews synthesize mouse input as touch events; a
    // mouse has no scroll conflict (scrolling uses the wheel), so the time gate is skipped and only
    // the displacement gate is kept to distinguish a click from a drag, avoiding jitter on a click
    // of a + button/arrow etc. mistakenly entering drag mode
    isMouse: boolean;
}

// Decide whether a slide should yield to native scrolling (instead of entering drag mode): if the
// displacement exceeds the threshold and it moves within the long-press gate window, mark it as
// a scroll.
// Returns true to yield to scrolling (no drag), false to allow entering drag mode
const shouldYieldToScroll = (gate: LongPressGate, clientX: number, clientY: number): boolean => {
    const dx = clientX - gate.startX;
    const dy = clientY - gate.startY;
    if (Math.abs(dx) < Constants.SIZE_DRAG_THRESHOLD && Math.abs(dy) < Constants.SIZE_DRAG_THRESHOLD) {
        // Displacement too small, keep waiting for the long-press decision
        return true;
    }
    if (gate.isMouse) {
        // A mouse has no scroll conflict (scrolling uses the wheel), so the finger's 400ms long-press
        // gate is skipped. But for elements like the file tree/gallery/list actions, the same
        // gesture can be either a click (+ button, arrow) or a drag, so a short minimum time is
        // still needed to tell them apart, avoiding click jitter mistakenly firing dragstart ->
        // the file tree adding disablehover -> the + button disappearing, children getting
        // pointer-events:none.
        // Elements such as the gutter marker with requireLongPress=false are meant to be dragged
        // right away on press, matching native desktop behavior
        if (gate.requireLongPress) {
            return Date.now() - gate.touchStartTime < Constants.TIMEOUT_MOUSE_DRAG_DELAY;
        }
        return false;
    }
    if (!gate.requireLongPress) {
        return false;
    }
    if (gate.longPressCancelled) {
        // Already decided to be a scroll
        return true;
    }
    if (Date.now() - gate.touchStartTime < Constants.TIMEOUT_LONGPRESS) {
        // Sliding within a short time counts as a scroll
        gate.longPressCancelled = true;
        return true;
    }
    return false;
};

interface TouchDragState {
    dataTransfer: DataTransfer | null;
    ghostElement: HTMLElement | null;
    isDragging: boolean;
    draggableElement: HTMLElement;
    editorElement: HTMLElement | null;
}

let dragState: (TouchDragState & LongPressGate) | null = null;
let lastDragOverElement: Element | null = null;

let manualState: (LongPressGate) | null = null;

// The input source of the most recent pointerdown; pointerType is the only reliable field to
// distinguish mouse/touch/pen
let lastPointerType: string = "";

// Decide whether the current input source is a mouse: some tablet WebViews synthesize mouse
// input as touch events.
// Treated as a mouse when pointerType === "mouse" and the contact area is 0 (radiusX/radiusY are 0).
// radiusX > 0 is reliable in one direction only: a nonzero value definitely means a real finger, so
// this vetoes the mouse decision, preventing a finger from being mistaken for a mouse and skipping
// the long press.
// force is not used (a real finger on iOS often reports 0), nor is sourceCapabilities (unsupported
// in WebKit)
const isMouseInput = (touch: Touch): boolean => {
    const hasContactArea = (touch.radiusX ?? 0) > 0 || (touch.radiusY ?? 0) > 0;
    return !hasContactArea && lastPointerType === "mouse";
};

// Whether the most recent input source was a mouse, used by event.ts to decide whether to
// synthesize a long-press context menu.
// A long left-mouse-button press should not trigger the context menu (that gesture is specific to
// touchscreen long-press-to-open-menu); a mouse's menu is triggered by the right button
export const isLastPointerMouse = (): boolean => {
    return lastPointerType === "mouse";
};

// Touch start: first check whether it hits the native Drag API (draggable="true"); if so, take
// the native path, otherwise check the manual mousedown allowlist
const handleTouchStart = (e: TouchEvent) => {
    if (dragState || manualState) return;
    if (e.touches.length !== 1) return;

    const target = e.target as HTMLElement;

    // Native Drag path: the element has a draggable="true" ancestor (e.g. the file tree, list
    // markers, AV row dragging), prefer the Drag API
    if (!target.classList.contains("av__widthdrag")) {
        const draggable = getDraggableAncestor(target);
        if (draggable) {
            const touch = e.touches[0];
            dragState = {
                dataTransfer: null,
                ghostElement: null,
                isDragging: false,
                draggableElement: draggable,
                editorElement: null,
                startX: touch.clientX,
                startY: touch.clientY,
                touchStartTime: Date.now(),
                // The file tree, gallery, tabs, and list actions require a long press, to avoid conflicting with scrolling
                requireLongPress: draggable.closest(".sy__file") !== null ||
                    draggable.closest(".sy__outline") !== null ||
                    draggable.closest(".av__gallery-item") !== null ||
                    draggable.closest(".layout-tab-bar") !== null ||
                    draggable.closest(".protyle-action") !== null,
                longPressCancelled: false,
                isMouse: isMouseInput(touch),
            };
            return;
        }
    }

    // The native <select> dropdown is drawn by the WebView as a system overlay; synthesizing a
    // mousedown would interfere with its touch sequence and cause the dropdown to flash and close
    // https://github.com/siyuan-note/siyuan/issues/17953
    if (target.tagName === "SELECT" || target.tagName === "OPTION" || target.closest("select")) {
        return;
    }
    // Manual mousedown path: areas that implement their own dragging, such as the dock / outline / resize handles
    if (!target.closest(".dock") &&
        // Inside a dialog, matching the whole .b3-dialog is not allowed, otherwise it breaks text
        // range-selection on flashcards https://github.com/siyuan-note/siyuan/issues/18055
        !(target.closest(".b3-dialog") &&  ["resize__move", "resize__rd", "resize__r", "resize__rt",
            "resize__d", "resize__l", "resize__ld", "resize__lt", "resize__t"].some(cls => target.closest("." + cls))) &&
        !target.closest(".sy__outline") &&
        !target.closest(".layout__resize") &&
        !target.closest(".layout__resize--lr") &&
        !target.closest(".layout__dockresize") &&
        !target.closest(".layout__dockresize--lr") &&
        !target.closest(".search__drag") &&
        // Editor-internal resize handles (not native Drag API)
        !target.closest(".av__widthdrag") &&
        !target.closest(".av__drag-fill") &&
        !target.closest(".protyle-action__drag") &&
        !target.closest(".table__resize") &&
        !target.closest(".sb__resize") &&
        !target.closest(".protyle-background__img") &&
        !target.closest(".b3-chip")) return;

    const touch = e.touches[0];
    const mouseEvent = new MouseEvent("mousedown", {
        bubbles: true,
        cancelable: true,
        clientX: touch.clientX,
        clientY: touch.clientY,
        button: 0,
        view: window,
    });
    target.dispatchEvent(mouseEvent);
    manualState = {
        startX: touch.clientX,
        startY: touch.clientY,
        touchStartTime: Date.now(),
        requireLongPress: target.closest(".sy__outline") !== null,
        longPressCancelled: false,
        isMouse: isMouseInput(touch),
    };
};

// Touch move: route to the native Drag or manual mousedown path depending on whether dragState or manualState is set
const handleTouchMove = (e: TouchEvent) => {
    // Native Drag path
    if (dragState) {
        const touch = e.touches[0];
        if (!dragState.isDragging) {
            // Long-press gate: for the file tree, gallery, list markers, etc, sliding shortly after a touch counts as scrolling and yields to native scroll
            if (shouldYieldToScroll(dragState, touch.clientX, touch.clientY)) {
                return;
            }
            e.preventDefault();
            startTouchDrag(touch);
            return;
        }
        e.preventDefault();
        continueTouchDrag(touch);
        return;
    }

    // Manual mousedown path
    if (!manualState) return;
    const touch = e.touches[0];
    if (!document.onmousemove || typeof document.onmousemove !== "function") return;

    // Long-press gate: for a scrollable list (e.g. the outline), sliding shortly after a touch
    // counts as scrolling and yields to native scroll, preventing scrolling from turning into a drag
    if (shouldYieldToScroll(manualState, touch.clientX, touch.clientY)) {
        return;
    }

    e.preventDefault();
    // Already in drag mode: set the flag so that on release, event.ts's long-press menu decision
    // returns early, avoiding drag and the menu both firing
    window.siyuan.touchDragActive = true;
    const elementUnderFinger = document.elementFromPoint(touch.clientX, touch.clientY);
    if (elementUnderFinger) {
        elementUnderFinger.dispatchEvent(new MouseEvent("mousemove", {
            clientX: touch.clientX,
            clientY: touch.clientY,
            cancelable: true,
            bubbles: true,
        }));
    }
};

// Touch end: the native path dispatches drop/dragend, the manual path dispatches mouseup to clean up
const handleTouchEnd = (e: TouchEvent) => {
    if (dragState) {
        if (dragState.isDragging) {
            e.preventDefault();
            endTouchDrag(e.changedTouches[0]);
        }
        cleanupDrag();
        return;
    }
    if (!manualState) return;
    // Dispatch mouseup to trigger the onmouseup cleanup callback registered by components (e.g.
    // Outline.bindSort), and reset the state
    cancelManualTouch();
};

const getDraggableAncestor = (el: HTMLElement): HTMLElement | null => {
    let current: HTMLElement | null = el;
    while (current) {
        if (current.getAttribute?.("draggable") === "true") {
            return current;
        }
        if (current === document.body) break;
        current = current.parentElement;
    }
    return null;
};

const getElementUnderTouch = (clientX: number, clientY: number): Element | null => {
    if (dragState?.ghostElement) {
        dragState.ghostElement.style.display = "none";
    }
    const el = document.elementFromPoint(clientX, clientY);
    if (dragState?.ghostElement) {
        dragState.ghostElement.style.display = "";
    }
    return el;
};

const positionGhost = (clientX: number, clientY: number) => {
    if (dragState?.ghostElement) {
        // Offset ghost so it's visible beside the finger, not hidden under it
        dragState.ghostElement.style.left = `${clientX + 12}px`;
        dragState.ghostElement.style.top = `${clientY + 12}px`;
    }
};

const clearDragoverClasses = () => {
    document.querySelectorAll(".dragover__top, .dragover__bottom, .dragover__left, .dragover__right, .dragover").forEach((item) => {
        item.classList.remove("dragover__top", "dragover__bottom", "dragover__left", "dragover__right", "dragover");
    });
};

const startTouchDrag = (touch: Touch) => {
    const dt = new DataTransfer();
    dragState.dataTransfer = dt;
    dragState.isDragging = true;

    dragState.editorElement = dragState.draggableElement.closest(".protyle-wysiwyg") as HTMLElement;

    window.siyuan.touchDragActive = true;
    window.siyuan.touchDragGhost = null;

    const dragStartEvent = new DragEvent("dragstart", {
        bubbles: true,
        cancelable: true,
        clientX: touch.clientX,
        clientY: touch.clientY,
        dataTransfer: dt,
        view: window,
    });
    dragState.draggableElement.dispatchEvent(dragStartEvent);

    dragState.ghostElement = window.siyuan.touchDragGhost || null;
    if (dragState.ghostElement) {
        dragState.ghostElement.style.pointerEvents = "none";
        dragState.ghostElement.style.zIndex = (++window.siyuan.zIndex).toString();
        // Position first, then show — avoids flash at wrong position
        positionGhost(touch.clientX, touch.clientY);
        dragState.ghostElement.style.opacity = "0.6";
    }

    if (dragState.editorElement) {
        const dragEnterEvent = new DragEvent("dragenter", {
            bubbles: false,
            cancelable: true,
            clientX: touch.clientX,
            clientY: touch.clientY,
            dataTransfer: dt,
            view: window,
        });
        dragState.editorElement.dispatchEvent(dragEnterEvent);
    }
};

const continueTouchDrag = (touch: Touch) => {
    if (!dragState.isDragging) return;

    const elementUnderTouch = getElementUnderTouch(touch.clientX, touch.clientY);

    // Track dragenter / dragleave across container-level elements.
    // Only dispatch when element's parent changes, to avoid flickering
    // when moving between siblings of the same parent.
    if (elementUnderTouch !== lastDragOverElement) {
        const prevContainer = lastDragOverElement?.parentElement;
        const currContainer = elementUnderTouch?.parentElement;
        if (prevContainer !== currContainer || (!prevContainer && currContainer) || (prevContainer && !currContainer)) {
            if (lastDragOverElement) {
                const dragLeaveEvent = new DragEvent("dragleave", {
                    bubbles: true,
                    cancelable: true,
                    clientX: touch.clientX,
                    clientY: touch.clientY,
                    dataTransfer: dragState.dataTransfer,
                    view: window,
                });
                lastDragOverElement.dispatchEvent(dragLeaveEvent);
            }
            if (elementUnderTouch) {
                const dragEnterEvent = new DragEvent("dragenter", {
                    bubbles: true,
                    cancelable: true,
                    clientX: touch.clientX,
                    clientY: touch.clientY,
                    dataTransfer: dragState.dataTransfer,
                    view: window,
                });
                elementUnderTouch.dispatchEvent(dragEnterEvent);
            }
        }
        lastDragOverElement = elementUnderTouch;
    }

    if (elementUnderTouch) {
        const dragOverEvent = new DragEvent("dragover", {
            bubbles: true,
            cancelable: true,
            clientX: touch.clientX,
            clientY: touch.clientY,
            dataTransfer: dragState.dataTransfer,
            view: window,
        });
        elementUnderTouch.dispatchEvent(dragOverEvent);
    }

    positionGhost(touch.clientX, touch.clientY);
};

const endTouchDrag = (touch: Touch) => {
    if (!dragState.isDragging) return;

    const elementUnderTouch = getElementUnderTouch(touch.clientX, touch.clientY);
    if (elementUnderTouch) {
        const dropEvent = new DragEvent("drop", {
            bubbles: true,
            cancelable: true,
            clientX: touch.clientX,
            clientY: touch.clientY,
            dataTransfer: dragState.dataTransfer,
            view: window,
        });
        elementUnderTouch.dispatchEvent(dropEvent);
    }

    const dragEndEvent = new DragEvent("dragend", {
        bubbles: true,
        cancelable: true,
        clientX: touch.clientX,
        clientY: touch.clientY,
        dataTransfer: dragState.dataTransfer,
        view: window,
    });
    dragState.draggableElement.dispatchEvent(dragEndEvent);

    clearDragoverClasses();
};

const cleanupDrag = () => {
    stopScrollAnimation();
    clearDragoverClasses();

    if (dragState?.ghostElement) {
        dragState.ghostElement.remove();
    }

    window.siyuan.touchDragActive = false;
    window.siyuan.touchDragGhost = null;
    dragState = null;
    lastDragOverElement = null;
};

const handleCancel = () => {
    // On touchcancel both paths must be cleaned up unconditionally (cleanupDrag/cancelManualTouch
    // both handle the empty-state case internally)
    cleanupDrag();
    cancelManualTouch();
};

// Cancel the manual bridge (mousedown) path: dispatch mouseup to trigger the cleanup callbacks
// registered by each component (e.g. Outline.bindSort's mouseup clears document.onmousemove etc.),
// and reset the state.
// event.ts's touchend unconditionally calls this first, ensuring onmousemove/onmouseup registered
// by things like Outline.bindSort do not linger and get mistakenly triggered by later events
// (creating a drag ghost, starting a scroll animation, etc.)
export const cancelManualTouch = () => {
    if (manualState && document.onmouseup && typeof document.onmouseup === "function") {
        document.onmouseup(new MouseEvent("mouseup", {bubbles: true}));
    }
    manualState = null;
    window.siyuan.touchDragActive = false;
};

export const initTouchDragBridge = () => {
    // Record the input source, so the touchstart callback can distinguish the case of a mouse
    // synthesized as touch (some tablets synthesize mouse input as touch events)
    document.addEventListener("pointerdown", (e: PointerEvent) => {
        lastPointerType = e.pointerType;
    }, {passive: true});

    // Touch event bridge: a unified entry point for the native Drag API (draggable="true") and
    // manual mousedown dragging (dock/outline/resize handles)
    document.addEventListener("touchstart", handleTouchStart, {passive: false});
    document.addEventListener("touchmove", handleTouchMove, {passive: false});
    document.addEventListener("touchend", handleTouchEnd);
    document.addEventListener("touchcancel", handleCancel);
};
