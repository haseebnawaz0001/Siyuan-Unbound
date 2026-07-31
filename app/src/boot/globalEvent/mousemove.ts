import {getAllEditor, getAllModels} from "../../layout/getAll";
import {isWindow} from "../../util/functions";
import {hasClosestBlock, hasClosestByClassName, hasClosestByTag} from "../../protyle/util/hasClosest";
import {getColIndex} from "../../protyle/util/table";

const getRightBlock = (element: HTMLElement, x: number, y: number) => {
    let left = x + 34;
    let nodeElement = element;
    if (nodeElement && nodeElement.classList.contains("protyle-action")) {
        return nodeElement;
    }
    let lastNodeElement;
    while (nodeElement && (
        nodeElement.classList.contains("list") || nodeElement.classList.contains("li") ||
        nodeElement.classList.contains("bq") || nodeElement.classList.contains("callout")
    )) {
        nodeElement = document.elementFromPoint(left, y) as HTMLElement;
        const calloutInfoElement = hasClosestByClassName(nodeElement, "callout-info");
        if (calloutInfoElement) {
            nodeElement = calloutInfoElement;
            break;
        }
        nodeElement = hasClosestBlock(nodeElement) as HTMLElement;
        if (lastNodeElement && lastNodeElement === nodeElement) {
            break;
        }
        lastNodeElement = nodeElement;
        if (nodeElement) {
            if (nodeElement.classList.contains("bq") || nodeElement.classList.contains("callout")) {
                left += 10;
            } else {
                left += 34;
            }
        } else {
            left += 34;
        }
    }
    return nodeElement;
};

export const windowMouseMove = (event: MouseEvent, mouseIsEnter: boolean) => {
    if (document.body.classList.contains("body--blur") || document.getElementById("progress")) {
        // Don't run while the window is inactive https://ld246.com/article/1693474547631
        return;
    }
    // https://github.com/siyuan-note/siyuan/pull/8793
    const coordinates = window.siyuan.coordinates ?? (window.siyuan.coordinates = {
        pageX: 0,
        pageY: 0,
        clientX: 0,
        clientY: 0,
        screenX: 0,
        screenY: 0,
    });
    coordinates.pageX = event.pageX;
    coordinates.pageY = event.pageY;
    coordinates.clientX = event.clientX;
    coordinates.clientY = event.clientY;
    coordinates.screenX = event.screenX;
    coordinates.screenY = event.screenY;

    // breadcrumb
    if (window.siyuan.hideBreadcrumb) {
        window.siyuan.hideBreadcrumb = false;
        getAllEditor().forEach(item => {
            if (item.protyle.breadcrumb?.element.classList.contains("protyle-breadcrumb__bar--hide")) {
                item.protyle.breadcrumb.element.classList.remove("protyle-breadcrumb__bar--hide");
                item.protyle.breadcrumb.render(item.protyle, true);
            }
        });
    }
    const target = event.target as Element;
    // Dock
    if (!mouseIsEnter &&
        event.buttons === 0 &&  // Don't trigger while a mouse button is pressed
        window.siyuan.layout.bottomDock &&
        !isWindow()) {
        if (event.clientX < Math.max(document.getElementById("dockLeft").clientWidth + 1, 16)) {
            if (!window.siyuan.layout.leftDock.pin && window.siyuan.layout.leftDock.layout.element.clientWidth > 0 &&
                // Hiding the dock bar would cause clicking content on either side to trigger the floating panel
                // to pop up, so the mouse detection range needs to be narrowed
                (window.siyuan.layout.leftDock.elements[0].clientWidth > 0 || (window.siyuan.layout.leftDock.elements[0].clientWidth === 0 && event.clientX < 8))) {
                if (event.clientY > document.getElementById("toolbar").clientHeight &&
                    event.clientY < window.innerHeight - document.getElementById("status").clientHeight) {
                    if (!hasClosestByClassName(target, "b3-menu") &&
                        !hasClosestByClassName(target, "protyle-toolbar") &&
                        !hasClosestByClassName(target, "protyle-util") &&
                        !hasClosestByClassName(target, "b3-dialog", true) &&
                        !hasClosestByClassName(target, "layout--float")) {
                        window.siyuan.layout.leftDock.showDock();
                    }
                } else {
                    window.siyuan.layout.leftDock.hideDock();
                }
            }
        } else if (event.clientX > window.innerWidth - Math.max(document.getElementById("dockRight").clientWidth - 2, 16)) {
            if (!window.siyuan.layout.rightDock.pin && window.siyuan.layout.rightDock.layout.element.clientWidth > 0 &&
                (window.siyuan.layout.rightDock.elements[0].clientWidth > 0 || (window.siyuan.layout.rightDock.elements[0].clientWidth === 0 && event.clientX > window.innerWidth - 8))) {
                if (event.clientY > document.getElementById("toolbar").clientHeight &&
                    event.clientY < window.innerHeight - document.getElementById("status").clientHeight) {
                    if (!hasClosestByClassName(target, "b3-menu") &&
                        !hasClosestByClassName(target, "layout--float") &&
                        !hasClosestByClassName(target, "protyle-toolbar") &&
                        !hasClosestByClassName(target, "protyle-util") &&
                        !hasClosestByClassName(target, "b3-dialog", true)) {
                        window.siyuan.layout.rightDock.showDock();
                    }
                } else {
                    window.siyuan.layout.rightDock.hideDock();
                }
            }
        }
        if (event.clientY > Math.min(window.innerHeight - 10, window.innerHeight - document.querySelector("#status").clientHeight)) {
            window.siyuan.layout.bottomDock.showDock();
        }
    }

    // gutter
    const eventPath0 = event.composedPath()[0] as HTMLElement;
    if (eventPath0 && eventPath0.nodeType !== 3 && eventPath0.classList.contains("protyle-wysiwyg") && eventPath0.style.paddingLeft) {
        // Also show it when the cursor is at the right edge of the editor
        const mouseElement = document.elementFromPoint(eventPath0.getBoundingClientRect().left + parseInt(eventPath0.style.paddingLeft) + 13, event.clientY);
        const blockElement = hasClosestBlock(mouseElement);
        if (blockElement) {
            const targetBlockElement = getRightBlock(blockElement, blockElement.getBoundingClientRect().left + 1, event.clientY);
            if (!targetBlockElement) {
                return;
            }
            const allModels = getAllModels();
            let findNode = false;
            allModels.editor.find(item => {
                if (item.editor.protyle.wysiwyg.element === eventPath0) {
                    item.editor.protyle.gutter.render(item.editor.protyle, targetBlockElement, mouseElement);
                    findNode = true;
                    return true;
                }
            });
            if (!findNode) {
                window.siyuan.blockPanels.find(item => {
                    item.editors.find(eItem => {
                        if (eItem.protyle.wysiwyg.element.contains(eventPath0)) {
                            eItem.protyle.gutter.render(eItem.protyle, targetBlockElement, mouseElement);
                            findNode = true;
                            return true;
                        }
                    });
                    if (findNode) {
                        return true;
                    }
                });
            }
            if (!findNode) {
                allModels.backlink.find(item => {
                    item.editors.find(eItem => {
                        if (eItem.protyle.wysiwyg.element === eventPath0) {
                            eItem.protyle.gutter.render(eItem.protyle, targetBlockElement, mouseElement);
                            findNode = true;
                            return true;
                        }
                    });
                    if (findNode) {
                        return true;
                    }
                });
            }
        }
        return;
    }
    if (eventPath0 && eventPath0.nodeType !== 3 && (
        eventPath0.classList.contains("li") ||
        eventPath0.classList.contains("list") ||
        (eventPath0.classList.contains("protyle-action") && eventPath0.parentElement.getAttribute("data-type") === "NodeListItem")
    )) {
        // When the cursor is in the lower part of a list, the element to the right should be shown instead of the
        // list itself
        const targetBlockElement = getRightBlock(eventPath0, eventPath0.getBoundingClientRect().left + 1, event.clientY);
        if (!targetBlockElement) {
            return;
        }
        const allModels = getAllModels();
        let findNode = false;
        allModels.editor.find(item => {
            if (item.editor.protyle.wysiwyg.element.contains(eventPath0)) {
                item.editor.protyle.gutter.render(item.editor.protyle, targetBlockElement);
                findNode = true;
                return true;
            }
        });
        if (!findNode) {
            window.siyuan.blockPanels.find(item => {
                item.editors.find(eItem => {
                    if (eItem.protyle.wysiwyg.element.contains(eventPath0)) {
                        eItem.protyle.gutter.render(eItem.protyle, targetBlockElement);
                        findNode = true;
                        return true;
                    }
                });
                if (findNode) {
                    return true;
                }
            });
        }
        if (!findNode) {
            allModels.backlink.find(item => {
                item.editors.find(eItem => {
                    if (eItem.protyle.wysiwyg.element.contains(eventPath0)) {
                        eItem.protyle.gutter.render(eItem.protyle, targetBlockElement);
                        findNode = true;
                        return true;
                    }
                });
                if (findNode) {
                    return true;
                }
            });
        }
        return;
    }

    if (eventPath0 && eventPath0.nodeType !== 3 && eventPath0.classList.contains("av")) {
        // The cursor is at the side of the attribute view when it is centered https://github.com/siyuan-note/siyuan/issues/13853
        if (eventPath0.getAttribute("data-type") === "NodeAttributeView") {
            const rowElement = hasClosestByClassName(document.elementFromPoint(eventPath0.firstElementChild.getBoundingClientRect().left + 10, event.clientY), "av__row");
            if (rowElement && !rowElement.classList.contains("av__row--header")) {
                getAllEditor().find(item => {
                    if (item.protyle.wysiwyg.element.contains(eventPath0)) {
                        item.protyle.gutter.render(item.protyle, eventPath0, rowElement);
                        return true;
                    }
                });
                return;
            }
        }
    }

    if (!hasClosestByClassName(target, "protyle", true)) {
        document.querySelectorAll(".protyle-gutters").forEach(item => {
            item.classList.add("fn__none");
            item.innerHTML = "";
        });
    }

    const blockElement = hasClosestByClassName(target, "table");
    if (blockElement && blockElement.style.cursor !== "col-resize" && !hasClosestByClassName(blockElement, "protyle-wysiwyg__embed")) {
        const cellElement = (hasClosestByTag(target, "TH") || hasClosestByTag(target, "TD")) as HTMLTableCellElement;
        const tableElement = blockElement.querySelector("table");
        if (cellElement && tableElement) {
            const resizeElement = blockElement.querySelector(".table__resize");
            if (blockElement.style.textAlign === "center" || blockElement.style.textAlign === "right") {
                resizeElement.parentElement.style.left = tableElement.offsetLeft + "px";
            } else {
                resizeElement.parentElement.style.left = "";
            }

            if (tableElement.getAttribute("contenteditable") === "true") {
                const tableHeight = blockElement.querySelector("colgroup").clientHeight;
                const captionElement = blockElement.querySelector("caption");
                const captionHeight = (captionElement && captionElement.style.captionSide !== "bottom") ? captionElement.clientHeight : 0;
                const rect = cellElement.getBoundingClientRect();
                if (rect.right - event.clientX < 3 && rect.right - event.clientX > 0) {
                    resizeElement.setAttribute("data-col-index", (getColIndex(cellElement) + cellElement.colSpan - 1).toString());
                    // Record the base left (excluding scrollLeft) so it can be repositioned after horizontal
                    // scrolling https://github.com/siyuan-note/siyuan/issues/13828
                    resizeElement.setAttribute("data-left", (cellElement.offsetWidth + cellElement.offsetLeft - 3).toString());
                    resizeElement.setAttribute("style", `top:${captionHeight}px;height:${tableHeight}px;left: ${Math.round(cellElement.offsetWidth + cellElement.offsetLeft - blockElement.firstElementChild.scrollLeft - 3)}px;display:block`);
                } else if (event.clientX - rect.left < 3 && event.clientX - rect.left > 0 && cellElement.previousElementSibling) {
                    resizeElement.setAttribute("data-col-index", (getColIndex(cellElement) - 1).toString());
                    resizeElement.setAttribute("data-left", (cellElement.offsetLeft - 3).toString());
                    resizeElement.setAttribute("style", `top:${captionHeight}px;height:${tableHeight}px;left: ${Math.round(cellElement.offsetLeft - blockElement.firstElementChild.scrollLeft - 3)}px;display:block`);
                }
            }
        }
    }
};
