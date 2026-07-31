import {isMobile} from "../util/functions";

export const showTooltip = (
    message: string,
    target: Element,
    tooltipClass?: string,
    event?: MouseEvent,
    space: number = 0.5,
) => {
    if (isMobile() || !message) {
        return;
    }
    let targetRect = target.getBoundingClientRect();
    // Element spanning multiple lines
    const clientRects = Array.from(target.getClientRects());
    if (clientRects.length > 1) {
        if (event) {
            // Choose the rect near the mouse
            clientRects.forEach(item => {
                if (event.clientY >= item.top - 3 && event.clientY <= item.bottom) {
                    targetRect = item;
                }
            });
        } else {
            // Choose the widest rect
            let lastWidth = 0;
            clientRects.forEach(item => {
                if (item.width > lastWidth) {
                    targetRect = item;
                }
                lastWidth = item.width;
            });
        }
    }
    if (targetRect.height === 0) {
        hideTooltip();
        return;
    }
    const messageElement = document.getElementById("tooltip");
    messageElement.className = tooltipClass ? `tooltip tooltip--${tooltipClass}` : "tooltip";
    messageElement.innerHTML = window.DOMPurify.sanitize(message);
    // Avoid the original top and left affecting the calculation
    messageElement.removeAttribute("style");
    const position = target.getAttribute("data-position");
    const parentRect = target.parentElement.getBoundingClientRect();

    let left;
    let top;
    if (position === "parentE") {
        // parentE: file tree and outline, backlink and viewcard
        top = Math.max(0, parentRect.top - (messageElement.clientHeight - parentRect.height) / 2);
        if (top > window.innerHeight - messageElement.clientHeight) {
            top = window.innerHeight - messageElement.clientHeight;
        }
        left = parentRect.right + 8;
        if (left + messageElement.clientWidth > window.innerWidth) {
            left = parentRect.left - messageElement.clientWidth - 8;
        }
    } else if (position === "parentW") {
        // ${number}parentW: attribute view & col & select
        top = Math.max(0, parentRect.top - (messageElement.clientHeight - parentRect.height) / 2);
        if (top > window.innerHeight - messageElement.clientHeight) {
            top = window.innerHeight - messageElement.clientHeight;
        }
        left = parentRect.left - messageElement.clientWidth;
        if (left < 0) {
            left = parentRect.right;
        }
    } else if (position?.endsWith("west")) {
        // west: gutter & title icon & av relation
        const positionDiff = parseInt(position) || space;
        top = Math.max(0, targetRect.top - (messageElement.clientHeight - targetRect.height) / 2);
        if (top > window.innerHeight - messageElement.clientHeight) {
            top = window.innerHeight - messageElement.clientHeight;
        }
        left = targetRect.left - messageElement.clientWidth - positionDiff;
        if (left < 0) {
            left = targetRect.right;
        }
    } else if (position?.endsWith("east")) {
        // east: layout menu
        const positionDiff = parseInt(position) || space;
        top = Math.max(0, targetRect.top - (messageElement.clientHeight - targetRect.height) / 2);
        if (top > window.innerHeight - messageElement.clientHeight) {
            top = window.innerHeight - messageElement.clientHeight;
        }
        left = targetRect.right + positionDiff;
        if (left + messageElement.clientWidth > window.innerWidth) {
            left = targetRect.left - messageElement.clientWidth - positionDiff;
        }
    } else if (position?.endsWith("north")) {
        // north: av view, column, multi-select description, protyle-icon
        const positionDiff = parseInt(position) || space;
        left = Math.max(0, targetRect.left - (messageElement.clientWidth - targetRect.width) / 2);
        top = targetRect.top - messageElement.clientHeight - positionDiff;
        if (top < 0) {
            if (targetRect.top < window.innerHeight - targetRect.bottom) {
                top = targetRect.bottom + positionDiff;
                messageElement.style.maxHeight = (window.innerHeight - top) + "px";
            } else {
                top = 0;
                messageElement.style.maxHeight = (targetRect.top - positionDiff) + "px";
            }
        }
        if (left + messageElement.clientWidth > window.innerWidth) {
            left = window.innerWidth - messageElement.clientWidth;
        }
    } else {
        // ${number}south & default
        const positionDiff = parseInt(position) || space;
        left = Math.max(0, targetRect.left - (messageElement.clientWidth - targetRect.width) / 2);
        top = targetRect.bottom + positionDiff;

        if (top + messageElement.clientHeight > window.innerHeight) {
            if (targetRect.top - positionDiff > window.innerHeight - top) {
                top = Math.max(0, targetRect.top - positionDiff - messageElement.clientHeight);
                messageElement.style.maxHeight = (targetRect.top - positionDiff) + "px";
            } else {
                messageElement.style.maxHeight = (window.innerHeight - top) + "px";
            }
        }
        if (left + messageElement.clientWidth > window.innerWidth) {
            left = window.innerWidth - messageElement.clientWidth;
        }
    }
    messageElement.style.top = top + "px";
    messageElement.style.left = Math.max(0, left) + "px";
    // Follows the same convention as data-position: the trigger element can specify a hover delay in milliseconds
    // via data-delay; if unset, the SCSS default is used
    const tooltipDelay = target.getAttribute("data-delay");
    if (tooltipDelay) {
        messageElement.style.animationDelay = tooltipDelay + "ms";
    }
};

export const hideTooltip = () => {
    document.getElementById("tooltip").classList.add("fn__none");
};
