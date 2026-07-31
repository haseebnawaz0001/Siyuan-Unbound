import {getTopBarHeight} from "../layout/getTopBarHeight";

export const setPosition = (element: HTMLElement, left: number, top: number, targetHeight = 0, targetLeft = 0, sticky = false) => {
    element.style.top = top + "px";
    element.style.left = left + "px";
    const rect = element.getBoundingClientRect();
    const topBarHeight = getTopBarHeight();
    if (rect.top < topBarHeight) {
        // If the element touches the top bar, move it down
        element.style.top = topBarHeight + "px";
    } else if (rect.bottom > window.innerHeight) {
        const y = top - rect.height - targetHeight;
        if (y > topBarHeight && (y + rect.height) < window.innerHeight) {
            // If the element's bottom overflows the window (not enough space below), move it up
            element.style.top = y + "px";
        } else {
            // If there is not enough space above or below, move it up but keep it as close to the bottom as possible
            element.style.top = Math.max(topBarHeight, window.innerHeight - rect.height) + "px";
        }
    }

    if (sticky) {
        // Sticky positioning: stays stable under the same anchor (top unchanged)
        // - When there is space below, prefer expanding downward (use the initial anchor top, and
        //   let the bottom naturally extend with the height)
        // - Only expand upward when there is no room below (lock the bottom edge, move the top up)
        // This way the menu neither jumps nor overflows as its height grows or shrinks
        const lockedBottom = element.dataset.positionBottom;
        const lockedX = element.dataset.positionX;
        const sameAnchor = element.dataset.positionTop === String(top);
        if (sameAnchor && lockedBottom !== undefined) {
            if (top + rect.height <= window.innerHeight) {
                // Fits below: expand downward, back to the initial anchor
                element.style.top = top + "px";
            } else {
                // Does not fit below: expand upward, locking the bottom edge
                const newTop = parseFloat(lockedBottom) - rect.height;
                element.style.top = (newTop >= topBarHeight ? newTop : topBarHeight) + "px";
            }
        }
        if (sameAnchor && lockedX !== undefined) {
            element.style.left = lockedX + "px";
        }

        // Horizontal overflow correction (only done when not locked)
        if (!(sameAnchor && lockedX !== undefined)) {
            if (rect.right > window.innerWidth) {
                element.style.left = window.innerWidth - rect.width - targetLeft + "px";
            } else if (rect.left < 0) {
                element.style.left = "0";
            }
        }

        element.dataset.positionTop = String(top);
        const actualRect = element.getBoundingClientRect();
        element.dataset.positionBottom = String(actualRect.bottom);
        element.dataset.positionX = String(parseFloat(element.style.left));
    } else {
        if (rect.right > window.innerWidth) {
            // Show it on the left side
            element.style.left = window.innerWidth - rect.width - targetLeft + "px";
        } else if (rect.left < 0) {
            // Still shown on the left side, just shifted to the right
            element.style.left = "0";
        }
    }
};
