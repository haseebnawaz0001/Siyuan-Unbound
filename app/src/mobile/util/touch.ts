import {
    hasClosestBlock,
    hasClosestByAttribute,
    hasClosestByClassName,
    hasTopClosestByClassName,
    isInEmbedBlock,
} from "../../protyle/util/hasClosest";
import {closeModel, closePanel} from "./closePanel";
import {popMenu} from "../menu";
import {activeBlur} from "./keyboardToolbar";
import {isChromeBrowser, isInAndroid, isInHarmony, isIPhone} from "../../protyle/util/compatibility";
import {getRangeByPoint} from "../../protyle/util/selection";
import {getCurrentEditor} from "../editor";
import {Constants} from "../../constants";
import {getEmbedChildOperationContext} from "../../protyle/wysiwyg/getBlock";

let clientX: number;
let clientY: number;
let xDiff: number;
let yDiff: number;
let time: number;
let firstDirection: "toLeft" | "toRight";
let firstXY: "x" | "y";
let lastClientX: number;    // records the last clientX when it doesn't match the initial direction
let scrollBlock: boolean;
let isFirstMove = true;
// Timer for entering multi-select via long press
let longPressTimer: number;

const popSide = (render = true) => {
    if (render) {
        document.getElementById("toolbarFile").dispatchEvent(new CustomEvent("click"));
    } else {
        activeBlur();
        document.getElementById("sidebar").style.transform = "translateX(0px)";
    }
};

// Clear the timer for entering multi-select via long press
const clearLongPress = () => {
    if (longPressTimer) {
        clearTimeout(longPressTimer);
        longPressTimer = undefined;
    }
};

export const handleTouchUp = () => {
    if (Date.now() - time < Constants.TIMEOUT_MULTIPLE_SELECT) {
        clearLongPress();
    }
};

export const handleTouchEnd = (event: TouchEvent) => {
    const target = event.target as HTMLElement;
    const currentTime = Date.now();
    const editor = getCurrentEditor();
    if (!isInHarmony() && !isInAndroid()) {
        handleTouchUp();
    }
    if (Math.abs(clientX - event.changedTouches[0].clientX) < Constants.SIZE_DRAG_THRESHOLD &&
        Math.abs(clientY - event.changedTouches[0].clientY) < Constants.SIZE_DRAG_THRESHOLD) {
        if (editor && editor.protyle.toolbar.isMultiSelectMode()) {
            if (longPressTimer) {
                event.stopImmediatePropagation();
                event.preventDefault();
                return;
            }
            // Multi-select mode
            window.getSelection()?.removeAllRanges();
            activeBlur();
            const blockElement = hasClosestBlock(target);
            if (blockElement) {
                // This press already triggered multi-select while held down, so on release don't toggle the
                // selection state, just consume this gesture
                blockElement.querySelectorAll(".protyle-wysiwyg--select").forEach(item => {
                    item.classList.remove("protyle-wysiwyg--select");
                });
                const blockParentElement = hasClosestByClassName(blockElement.parentElement, "protyle-wysiwyg--select");
                if (blockParentElement) {
                    blockParentElement.classList.remove("protyle-wysiwyg--select");
                }
                blockElement.classList.toggle("protyle-wysiwyg--select");
                editor.protyle.toolbar.subElement.querySelector(".multiSelectCount").textContent =
                    editor.protyle.wysiwyg.element.querySelectorAll(".protyle-wysiwyg--select").length.toString();
                event.stopImmediatePropagation();
                event.preventDefault();
            }
        } else if (currentTime - time > Constants.TIMEOUT_LONGPRESS) {
            // Long press: multi-select was already triggered once the hold threshold was reached; cancel the timer
            // here to avoid triggering it again
            if (isIPhone() && !isChromeBrowser() && !window.siyuan.touchDragActive) {
                target.dispatchEvent(new MouseEvent("contextmenu", {
                    bubbles: true,
                    cancelable: true,
                    clientX: event.changedTouches[0].clientX,
                    clientY: event.changedTouches[0].clientY,
                }));
            }
            event.stopImmediatePropagation();
            event.preventDefault();
            return;
        }
    }
    if (typeof yDiff === "undefined" && editor?.protyle.options.render.gutter) {
        const nodeElement = hasClosestBlock(target);
        if (nodeElement && nodeElement.closest(".protyle-wysiwyg")) {
            if (nodeElement.classList.contains("list") || nodeElement.classList.contains("li")) {
                // When the cursor is in the lower part of a list, the element to the right should be shown instead
                // of the list itself; this is handled under mousemove in windowEvent
                return;
            }
            const embedElement = isInEmbedBlock(nodeElement);
            if (embedElement) {
                editor.protyle.gutter.render(editor.protyle,
                    getEmbedChildOperationContext(nodeElement) ? nodeElement : embedElement, target);
                return;
            }
            editor.protyle.gutter.render(editor.protyle, nodeElement, target);
        }
    }
    isFirstMove = true;
    if (!clientY || typeof yDiff === "undefined" ||
        target.tagName === "AUDIO" ||
        hasClosestByClassName(target, "b3-dialog", true) ||
        (window.siyuan.mobile.editor && !window.siyuan.mobile.editor.protyle.toolbar.subElement.classList.contains("fn__none")) ||
        hasClosestByClassName(target, "viewer-container") ||
        hasClosestByClassName(target, "keyboard") ||
        hasClosestByAttribute(target, "id", "commonMenu")
    ) {
        return;
    }
    if (window.siyuan.mobile.editor) {
        window.siyuan.mobile.editor.protyle.contentElement.style.overflow = "";
    }

    // Some events don't go through touchstart and touchmove, so set this to null to stop further processing
    clientX = null;
    // Some events don't go through touchmove

    if (scrollBlock) {
        closePanel();
        return;
    }

    let scrollEnable = false;
    if (Date.now() - time < 1000) {
        scrollEnable = true;
    } else if (Math.abs(xDiff) > window.innerWidth / 3) {
        scrollEnable = true;
    }

    const isXScroll = Math.abs(xDiff) > Math.abs(yDiff);
    const modelElement = hasClosestByAttribute(target, "id", "model", true);
    if (modelElement) {
        if (isXScroll && firstDirection === "toRight" && !lastClientX && !hasClosestByClassName(target, "protyle-wysiwyg", true) &&
            // Don't trigger closing the panel while selecting text
            (getSelection().rangeCount === 0 || getSelection().toString() === "")) {
            closeModel();
        }
        return;
    }
    const menuElement = hasClosestByAttribute(target, "id", "menu");
    if (menuElement) {
        if (isXScroll) {
            if (firstDirection === "toRight") {
                if (lastClientX) {
                    popMenu();
                } else {
                    closePanel();
                }
            } else {
                if (lastClientX) {
                    closePanel();
                } else {
                    popMenu();
                }
            }
        } else {
            popMenu();
        }
        return;
    }
    const sideElement = hasClosestByAttribute(target, "id", "sidebar");
    if (sideElement) {
        if (isXScroll) {
            if (firstDirection === "toLeft") {
                if (lastClientX) {
                    popSide(false);
                } else {
                    closePanel();
                }
            } else {
                if (lastClientX) {
                    closePanel();
                } else {
                    popSide(false);
                }
            }
        } else {
            popSide(false);
        }
        return;
    }
    if (!scrollEnable || !isXScroll) {
        closePanel();
        return;
    }

    if (xDiff > 0) {
        if (lastClientX) {
            closePanel();
        } else {
            popMenu();
        }
    } else {
        if (lastClientX) {
            closePanel();
        } else {
            popSide();
        }
    }
};

export const handleTouchStart = (event: TouchEvent) => {
    time = Date.now();
    const target = event.touches[0].target as HTMLElement;
    if (0 < event.touches.length && (target.tagName === "VIDEO" || target.tagName === "AUDIO")) {
        // https://github.com/siyuan-note/siyuan/issues/14569
        activeBlur();
        return;
    }
    // When another draggable element is present
    const otherTouchElement = hasClosestByClassName(target, "b3-chip");
    if ((otherTouchElement && otherTouchElement.parentElement.classList.contains("b3-chips__doctag")) ||
        target.closest(".protyle-gutters") ||
        target.closest(".protyle-action") ||
        target.closest(".av__gallery") ||
        (target.tagName === "IMG" && target.style.cursor === "move" && target.parentElement.classList.contains("protyle-background__img"))) {
        clientX = null;
        clientY = null;
        return;
    }
    const editor = getCurrentEditor();
    if (getSelection().rangeCount > 0 && hasClosestBlock(event.target as Element) &&
        editor && !editor.protyle.disabled && event.touches[0].clientY > window.innerHeight / 2 &&
        document.querySelector("#keyboardToolbar").classList.contains("fn__none")) {
        window.siyuan.mobile.touchRange = getRangeByPoint(event.touches[0].clientX, event.touches[0].clientY);
    }

    firstDirection = null;
    xDiff = undefined;
    yDiff = undefined;
    lastClientX = undefined;
    firstXY = undefined;
    if (isIPhone() ||
        (event.touches[0].clientX > 8 && event.touches[0].clientX < window.innerWidth - 8)) {
        clientX = event.touches[0].clientX;
        clientY = event.touches[0].clientY;
    } else {
        clientX = null;
        clientY = null;
        event.stopImmediatePropagation();
    }
    isFirstMove = true;
    scrollBlock = false;
    // When a long press on a block inside the editor reaches the threshold, enter multi-select mode directly
    // without needing to release
    clearLongPress();
    if (clientX && clientY && editor && !editor.protyle.toolbar.isMultiSelectMode()) {
        const blockElement = hasClosestBlock(target);
        if (blockElement && editor.protyle.wysiwyg.element.contains(blockElement)) {
            longPressTimer = window.setTimeout(() => {
                window.getSelection()?.removeAllRanges();
                editor.protyle.toolbar.showMultiSelectMode(editor.protyle, blockElement);
                if (editor.protyle.options.render.gutter) {
                    editor.protyle.gutter.render(editor.protyle, blockElement, target);
                }
            }, Constants.TIMEOUT_MULTIPLE_SELECT);
        }
    }
};

let previousClientX: number;
const sideMaskElement = document.querySelector(".side-mask") as HTMLElement;
export const handleTouchMove = (event: TouchEvent) => {
    const target = event.target as HTMLElement;
    // Movement exceeding the threshold means this is a swipe rather than a long press, so cancel the timer for
    // entering multi-select
    if (clientX && clientY &&
        (Math.abs(clientX - event.touches[0].clientX) >= 5 || Math.abs(clientY - event.touches[0].clientY) >= 5)) {
        clearLongPress();
    }
    if (!clientX || !clientY ||
        target.tagName === "AUDIO" ||
        document.getElementById("dragGhost") ||
        hasClosestByClassName(target, "b3-dialog", true) ||
        (window.siyuan.mobile.editor && !window.siyuan.mobile.editor.protyle.toolbar.subElement.classList.contains("fn__none")) ||
        hasClosestByClassName(target, "keyboard") ||
        hasClosestByClassName(target, "viewer-container") ||
        hasClosestByAttribute(target, "id", "commonMenu") || firstXY === "y"
    ) {
        return;
    }

    // Disable swiping while editing
    if (!document.querySelector("#keyboardToolbar").classList.contains("fn__none")) {
        return;
    }
    // Disable swiping when content is selected in read-only state
    if (getSelection().rangeCount > 0) {
        // Case where the selection was extended after selecting
        const range = getSelection().getRangeAt(0);
        const currentEditor = getCurrentEditor();
        if (range.toString() !== "" && currentEditor?.protyle.wysiwyg.element.contains(range.startContainer)) {
            return;
        }
    }

    xDiff = Math.floor(clientX - event.touches[0].clientX);
    yDiff = Math.floor(clientY - event.touches[0].clientY);
    if (!firstDirection) {
        firstDirection = xDiff > 0 ? "toLeft" : "toRight";
    }
    // Vertical scrolling prevents horizontal swiping
    if (!firstXY) {
        if (Math.abs(xDiff) > Math.abs(yDiff)) {
            firstXY = "x";
        } else {
            firstXY = "y";
        }
        if (firstXY === "x") {
            if ((hasClosestByAttribute(target, "id", "menu") && firstDirection === "toLeft") ||
                (hasClosestByAttribute(target, "id", "sidebar") && firstDirection === "toRight")) {
                firstXY = "y";
                yDiff = undefined;
            }
        }
    }
    if (previousClientX) {
        if (firstDirection === "toRight") {
            if (previousClientX > event.touches[0].clientX) {
                lastClientX = event.touches[0].clientX;
            } else {
                lastClientX = undefined;
            }
        } else if (firstDirection === "toLeft") {
            if (previousClientX < event.touches[0].clientX) {
                lastClientX = event.touches[0].clientX;
            } else {
                lastClientX = undefined;
            }
        }
    }
    previousClientX = event.touches[0].clientX;
    if (Math.abs(xDiff) > Math.abs(yDiff)) {
        if (hasClosestByAttribute(target, "id", "model", true)) {
            return;
        }
        if (sideMaskElement.classList.contains("fn__none")) {
            let scrollElement = hasClosestByAttribute(target, "data-type", "NodeCodeBlock");
            if (event.touches.length > 1 || (scrollElement && !scrollElement.classList.contains("code-block"))) {
                scrollBlock = true;
                return;
            }
            if (!scrollElement) {
                scrollElement = hasClosestByAttribute(target, "data-type", "NodeAttributeView") ||
                    hasClosestByAttribute(target, "data-type", "NodeMathBlock") ||
                    hasClosestByAttribute(target, "data-type", "NodeTable") ||
                    hasTopClosestByClassName(target, "list") ||
                    hasTopClosestByClassName(target, "protyle-breadcrumb__bar--nowrap");
            }
            if (scrollElement) {
                if (scrollElement.classList.contains("table")) {
                    scrollElement = scrollElement.firstElementChild as HTMLElement;
                } else if (scrollElement.classList.contains("code-block")) {
                    scrollElement = scrollElement.firstElementChild.nextElementSibling as HTMLElement;
                } else if (scrollElement.classList.contains("av")) {
                    scrollElement = hasClosestByClassName(target, "layout-tab-bar") || hasClosestByClassName(target, "av__scroll") ||
                        hasClosestByClassName(target, "av__kanban");
                } else if (scrollElement.dataset.type === "NodeMathBlock") {
                    while (scrollElement && scrollElement.nodeType === 1) {
                        if (scrollElement.scrollWidth > scrollElement.clientWidth) {
                            break;
                        }
                        scrollElement = scrollElement.firstElementChild as HTMLElement;
                    }
                }
                if (scrollElement && (
                    (xDiff < 0 && scrollElement.scrollLeft > 0) ||
                    (xDiff > 0 && Math.ceil(scrollElement.clientWidth + scrollElement.scrollLeft) < scrollElement.scrollWidth)
                )) {
                    scrollBlock = true;
                }
                if (scrollBlock) {
                    return;
                }
            }
        }

        if (isFirstMove) {
            sideMaskElement.style.zIndex = (++window.siyuan.zIndex).toString();
            document.getElementById("sidebar").style.zIndex = (++window.siyuan.zIndex).toString();
            document.getElementById("menu").style.zIndex = (++window.siyuan.zIndex).toString();
            isFirstMove = false;
        }
        const windowWidth = window.innerWidth;
        const menuElement = hasClosestByAttribute(target, "id", "menu");
        if (menuElement) {
            if (xDiff < 0) {
                menuElement.style.transform = `translateX(${-xDiff}px)`;
                transformMask(-xDiff / windowWidth);
            } else {
                menuElement.style.transform = "translateX(0px)";
                transformMask(0);
            }
            return;
        }
        const sideElement = hasClosestByAttribute(target, "id", "sidebar");
        if (sideElement) {
            if (xDiff > 0) {
                sideElement.style.transform = `translateX(${-xDiff}px)`;
                transformMask(xDiff / windowWidth);
            } else {
                sideElement.style.transform = "translateX(0px)";
                transformMask(0);
            }
            return;
        }

        if (firstDirection === "toRight") {
            document.getElementById("sidebar").style.transform = `translateX(${Math.min(-xDiff - windowWidth, 0)}px)`;
            transformMask((windowWidth + xDiff) / windowWidth);
        } else {
            document.getElementById("menu").style.transform = `translateX(${Math.max(windowWidth - xDiff, 0)}px)`;
            transformMask((windowWidth - xDiff) / windowWidth);
        }
        activeBlur();
        if (window.siyuan.mobile.editor) {
            window.siyuan.mobile.editor.protyle.contentElement.style.overflow = "hidden";
        }
    }
};

const transformMask = (opacity: number) => {
    sideMaskElement.classList.remove("fn__none");
    sideMaskElement.style.opacity = Math.min((1 - opacity), 0.68).toString();
};
