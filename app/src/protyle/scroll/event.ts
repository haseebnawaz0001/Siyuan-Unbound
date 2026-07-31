import {Constants} from "../../constants";
import {hideElements} from "../ui/hideElements";
import {fetchPost} from "../../util/fetch";
import {onGet} from "../util/onGet";
import {isMobile} from "../../util/functions";
import {hasClosestBlock, hasClosestByClassName} from "../util/hasClosest";
import {stickyRow} from "../render/av/row";
import {trimAVRowsSync} from "../render/av/virtualScroll";
import {isEncryptedBox} from "../../util/pathName";

let getIndexTimeout: number;
const avScrollPending = new WeakSet<HTMLElement>();
export const scrollEvent = (protyle: IProtyle, element: HTMLElement) => {
    element.addEventListener("scroll", () => {
        const elementRect = element.getBoundingClientRect();
        if (!protyle.toolbar.element.classList.contains("fn__none")) {
            const initY = protyle.toolbar.element.getAttribute("data-inity").split(Constants.ZWSP);
            const top = parseInt(initY[0]) + (parseInt(initY[1]) - element.scrollTop);
            if (top < elementRect.top - protyle.toolbar.toolbarHeight || top > elementRect.bottom - protyle.toolbar.toolbarHeight) {
                protyle.toolbar.element.style.display = "none";
            } else {
                protyle.toolbar.element.style.top = top + "px";
                protyle.toolbar.element.style.display = "";
            }
        }

        protyle.wysiwyg.element.querySelectorAll(".av").forEach((item: HTMLElement) => {
            if (item.dataset.render !== "true") {
                return;
            }
            // stickyRow and trimAVRows are merged into one rAF per block per frame: stickyRow first
            // (mostly reads layout), then trimAVRowsSync (adds/removes rows). Merging them avoids
            // interleaved reads/writes across two separate rAF callbacks triggering reflow;
            // reading before writing avoids trim's DOM writes turning sticky's geometry reads into a forced reflow.
            if (avScrollPending.has(item)) {
                return;
            }
            avScrollPending.add(item);
            requestAnimationFrame(() => {
                avScrollPending.delete(item);
                stickyRow(item, element, "all");
                trimAVRowsSync(item, elementRect);
            });
        });

        if (!protyle.element.classList.contains("block__edit") && !isMobile()) {
            protyle.contentElement.setAttribute("data-scrolltop", element.scrollTop.toString());
        }

        if (!window.siyuan.dragElement) { // https://ld246.com/article/1649638389841
            hideElements(["gutterOnly"], protyle);
        }

        if (protyle.scroll && !protyle.scroll.element.classList.contains("fn__none")) {
            clearTimeout(getIndexTimeout);
            getIndexTimeout = window.setTimeout(() => {
                let targetElement = document.elementFromPoint(elementRect.left + elementRect.width / 2, elementRect.top + 10);
                if (targetElement.classList.contains("protyle-wysiwyg")) {
                    // When it happens to land exactly in the middle of a block
                    targetElement = document.elementFromPoint(elementRect.left + elementRect.width / 2, elementRect.top + 20);
                }
                const blockElement = hasClosestBlock(targetElement);
                if (!blockElement) {
                    if ((protyle.wysiwyg.element.firstElementChild.getAttribute("data-eof") === "1" ||
                            // data-eof isn't 1 when going home
                            protyle.wysiwyg.element.firstElementChild.getAttribute("data-node-index") === "0") &&
                        (hasClosestByClassName(targetElement, "protyle-background") || hasClosestByClassName(targetElement, "protyle-title"))) {
                        const inputElement = protyle.scroll.element.querySelector(".b3-slider") as HTMLInputElement;
                        inputElement.value = "1";
                        protyle.scroll.element.setAttribute("aria-label", `Blocks 1/${protyle.block.blockCount}`);
                    }
                    return;
                }
                protyle.scroll.updateIndex(protyle, blockElement.getAttribute("data-node-id"));
            }, Constants.TIMEOUT_LOAD);
        }

        if (protyle.wysiwyg.element.getAttribute("data-top") || protyle.block.showAll ||
            (protyle.scroll && protyle.scroll.element.classList.contains("fn__none")) || !protyle.scroll ||
            protyle.scroll.lastScrollTop === element.scrollTop || protyle.scroll.lastScrollTop === -1 ||
            // On mobile, navigating sets wysiwyg.element.innerHTML = "";
            !protyle.wysiwyg.element.firstElementChild) {
            return;
        }
        if (protyle.scroll.lastScrollTop > element.scrollTop) {
            if (element.scrollTop === 0) {
                // scrollTop cannot be obtained accurately while dragging the scrollbar with the mouse, so it's ignored here
                return;
            }
            if (element.scrollTop < element.clientHeight &&
                protyle.wysiwyg.element.firstElementChild.getAttribute("data-eof") !== "1") {
                // Disabling scroll would cause jitter https://ld246.com/article/1666717094418
                protyle.contentElement.style.width = (protyle.contentElement.offsetWidth) + "px";
                protyle.contentElement.style.overflow = "hidden";
                protyle.wysiwyg.element.setAttribute("data-top", element.scrollTop.toString());
                const getDocParam: IObject = {
                    id: protyle.wysiwyg.element.firstElementChild.getAttribute("data-node-id"),
                    mode: 1,
                    size: window.siyuan.config.editor.dynamicLoadBlocks,
                };
                if (isEncryptedBox(protyle.notebookId)) {
                    getDocParam.notebook = protyle.notebookId;
                }
                fetchPost("/api/filetree/getDoc", getDocParam, getResponse => {
                    protyle.contentElement.style.overflow = "";
                    protyle.contentElement.style.width = "";
                    onGet({
                        data: getResponse,
                        protyle,
                        action: [Constants.CB_GET_BEFORE, Constants.CB_GET_UNCHANGEID],
                    });
                });
            }
        } else if ((element.scrollTop > element.scrollHeight - element.clientHeight * 1.8) &&
            protyle.wysiwyg.element.lastElementChild &&
            protyle.wysiwyg.element.lastElementChild.getAttribute("data-eof") !== "2") {
            if (protyle.scroll.lastScrollTop > 768 && element.scrollTop > protyle.scroll.lastScrollTop * 2) {
                // Correction needed for loading triggered by dragging the scrollbar with the mouse
                element.scrollTop = protyle.scroll.lastScrollTop;
                return;
            }
            protyle.wysiwyg.element.setAttribute("data-top", element.scrollTop.toString());
            const getDocParam: IObject = {
                id: protyle.wysiwyg.element.lastElementChild.getAttribute("data-node-id"),
                mode: 2,
                size: window.siyuan.config.editor.dynamicLoadBlocks,
            };
            if (isEncryptedBox(protyle.notebookId)) {
                getDocParam.notebook = protyle.notebookId;
            }
            fetchPost("/api/filetree/getDoc", getDocParam, getResponse => {
                onGet({
                    data: getResponse,
                    protyle,
                    action: [Constants.CB_GET_APPEND, Constants.CB_GET_UNCHANGEID],
                });
            });
        }
        protyle.scroll.lastScrollTop = Math.max(element.scrollTop, 0);
    }, {
        capture: false,
        passive: true,
        once: false
    });
};
