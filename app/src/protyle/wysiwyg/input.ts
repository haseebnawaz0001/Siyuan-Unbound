import {focusBlock, focusByWbr} from "../util/selection";
import {Constants} from "../../constants";
import * as dayjs from "dayjs";
import {transaction, updateTransaction} from "./transaction";
import {mathRender} from "../render/mathRender";
import {highlightRender} from "../render/highlightRender";
import {
    fixAdjacentTags,
    getContenteditableElement,
    getNextBlockSibling,
    getParentBlock,
    getPreviousBlockSibling,
    hasNextSibling,
    hasPreviousSibling,
    isNotEditBlock
} from "./getBlock";
import {genEmptyBlock} from "../../block/util";
import {blockRender} from "../render/blockRender";
import {hideElements} from "../ui/hideElements";
import {hasClosestByAttribute, hasClosestByClassName} from "../util/hasClosest";
import {fetchPost, fetchSyncPost} from "../../util/fetch";
import {headingTurnIntoList, turnIntoTaskList} from "./turnIntoList";
import {updateAVName} from "../render/av/action";
import {setFold} from "../util/blockFold";

export const input = async (protyle: IProtyle, blockElement: HTMLElement, range: Range, needRender = true, event?: InputEvent) => {
    if (!blockElement.parentElement) {
        // On different Windows versions, the IME can fire input multiple times, causing the block assigned by
        // outerHTML to be lost
        return;
    }
    if (blockElement.classList.contains("av")) {
        const avCursorElement = hasClosestByClassName(range.startContainer, "av__cursor");
        if (avCursorElement) {
            avCursorElement.textContent = Constants.ZWSP;
        } else {
            updateAVName(protyle, blockElement);
        }
        return;
    }
    const editElement = getContenteditableElement(blockElement) as HTMLElement;
    const type = blockElement.getAttribute("data-type");
    if (!editElement) {
        // Typing is not allowed in hr/embed blocks/math formulas/iframe/audio/video/chart render blocks, etc.
        // https://github.com/siyuan-note/siyuan/issues/3958
        if (type === "NodeThematicBreak") {
            blockElement.innerHTML = "<div><wbr></div>";
        } else if (type === "NodeBlockQueryEmbed") {
            blockElement.lastElementChild.previousElementSibling.innerHTML = "<wbr>" + Constants.ZWSP;
        } else if (type === "NodeMathBlock" || type === "NodeHTMLBlock") {
            // https://github.com/siyuan-note/siyuan/issues/15761
            if (blockElement.firstElementChild.firstChild.nodeType === 3) {
                blockElement.firstElementChild.firstChild.remove();
            }
            blockElement.lastElementChild.previousElementSibling.lastElementChild.innerHTML = "<wbr>" + Constants.ZWSP;
        } else if (type === "NodeIFrame" || type === "NodeWidget") {
            blockElement.innerHTML = "<wbr>" + blockElement.firstElementChild.outerHTML + blockElement.lastElementChild.outerHTML;
        } else if (type === "NodeVideo") {
            blockElement.firstElementChild.innerHTML = "<wbr>" + Constants.ZWSP + blockElement.firstElementChild.firstElementChild.outerHTML + blockElement.firstElementChild.lastElementChild.outerHTML;
        } else if (type === "NodeAudio") {
            blockElement.firstElementChild.innerHTML = blockElement.firstElementChild.firstElementChild.outerHTML + "<wbr>" + Constants.ZWSP;
        } else if (type === "NodeCodeBlock") {
            range.startContainer.textContent = Constants.ZWSP;
        }
        focusByWbr(blockElement, range);
        return;
    }
    blockElement.setAttribute("updated", dayjs().format("YYYYMMDDHHmmss"));
    const wbrElement: HTMLElement = document.createElement("wbr");
    range.insertNode(wbrElement);
    if (event) {
        const wbrNextElement = hasNextSibling(wbrElement) as HTMLElement;
        // A soft line break right before inline code: the caret should end up before the inline code
        if (!hasPreviousSibling(wbrElement) && wbrElement.parentElement.tagName === "SPAN" &&
            wbrNextElement && wbrNextElement.textContent.startsWith(Constants.ZWSP)) {
            wbrElement.parentElement.before(wbrElement);
        }
        if (event.inputType === "deleteContentForward") {
            if (wbrNextElement && wbrNextElement.nodeType === 1 && !wbrNextElement.textContent.startsWith(Constants.ZWSP)) {
                const nextType = (wbrNextElement.getAttribute("data-type") || "").split(" ");
                if (nextType.includes("code") || nextType.includes("kbd") || nextType.includes("tag")) {
                    wbrNextElement.insertAdjacentElement("afterbegin", wbrElement);
                }
            }
        }
        // https://github.com/siyuan-note/siyuan/issues/12468
        if ((event.inputType === "deleteContentBackward" || event.inputType === "deleteContentForward") &&
            wbrNextElement && wbrNextElement.nodeType === 1 && wbrNextElement.tagName === "BR") {
            // https://github.com/siyuan-note/siyuan/issues/13190
            const brNextElement = hasNextSibling(wbrNextElement);
            if (brNextElement && brNextElement.nodeType === 1 &&
                (brNextElement as HTMLElement).getAttribute("data-type")?.indexOf("inline-math") > -1) {
                wbrNextElement.remove();
            }
            // https://github.com/siyuan-note/siyuan/issues/14290
            if (event.inputType === "deleteContentBackward" &&
                wbrNextElement.previousSibling.previousSibling?.textContent.endsWith("\n")) {
                wbrNextElement.outerHTML = "\n";
            }
        }
    }
    const id = blockElement.getAttribute("data-node-id");
    if ((type !== "NodeCodeBlock" && type !== "NodeHeading") && // https://github.com/siyuan-note/siyuan/issues/11851
        (editElement.innerHTML.endsWith("\n<wbr>") || editElement.innerHTML.endsWith("\n<wbr>\n"))) {
        // Soft line break
        updateTransaction(protyle, blockElement, protyle.wysiwyg.lastHTMLs[id] || blockElement.outerHTML.replace("\n<wbr>", "<wbr>"));
        wbrElement.remove();
        return;
    }
    if (turnIntoTaskList(protyle, type, blockElement, editElement, range)) {
        return;
    }
    if (headingTurnIntoList(protyle, type, blockElement, editElement, range)) {
        return;
    }
    // Tables and bold text can also have a br; this is only for the br produced after deleting something like #a#
    const brElement = blockElement.querySelector("br");
    if (brElement && brElement.parentElement.tagName !== "TD" && brElement.parentElement.tagName !== "TH" && (
        brElement.parentElement.textContent.trim() === "" ||
        brElement.previousSibling?.previousSibling?.textContent === "\n"
    )) {
        brElement.remove();
    }

    if (type !== "NodeHeading" && (
        editElement.innerHTML.startsWith("》<wbr>") ||
        editElement.innerHTML.startsWith(Constants.ZWSP + "》<wbr>") ||
        editElement.innerHTML.indexOf("\n》<wbr>") > -1
    )) {
        editElement.innerHTML = editElement.innerHTML.replace("》<wbr>", "><wbr>");
    }
    const trimStartHTML = editElement.innerHTML.trimStart();
    const trimStartText = editElement.textContent.trimStart();
    if ((trimStartHTML.startsWith("````") || trimStartHTML.startsWith("····") || trimStartHTML.startsWith("~~~~")) &&
        trimStartHTML.indexOf("\n") === -1) {
        // More than three marker characters can form a code block; handled further below
    } else if ((trimStartHTML.startsWith("```") || trimStartHTML.startsWith("···") || trimStartHTML.startsWith("~~~")) &&
        trimStartHTML.indexOf("\n") === -1 && trimStartHTML.replace(/·|~/g, "`").replace(/^`{3,}/g, "").indexOf("`") === -1) {
        // ```test` is handled later; ```test is left alone
        updateTransaction(protyle, blockElement, protyle.wysiwyg.lastHTMLs[id]);
        wbrElement.remove();
        return;
    }
    // https://github.com/siyuan-note/siyuan/issues/9015
    if (type === "NodeParagraph" && (
        editElement.innerHTML.startsWith("¥¥<wbr>") || editElement.innerHTML.startsWith("￥￥<wbr>") ||
        // https://ld246.com/article/1730020516427
        trimStartHTML.indexOf("\n¥¥<wbr>") > -1 || trimStartHTML.indexOf("\n￥￥<wbr>") > -1
    )) {
        editElement.innerHTML = editElement.innerHTML.replace("¥¥<wbr>", "$$$$<wbr>").replace("￥￥<wbr>", "$$$$<wbr>");
    }

    const refElement = hasClosestByAttribute(range.startContainer, "data-type", "block-ref");
    if (refElement && refElement.getAttribute("data-subtype") === "d") {
        const response = await fetchSyncPost("/api/block/getRefText", {id: refElement.getAttribute("data-id")});
        if (response.data !== refElement.innerHTML.replace("<wbr>", "")) {
            refElement.setAttribute("data-subtype", "s");
        }
    }
    // Insert a space between adjacent tags as a separator, to avoid SpinBlockDOM merging them into a single tag
    // during parsing https://github.com/siyuan-note/siyuan/issues/18191
    fixAdjacentTags(editElement);
    let html = blockElement.outerHTML;
    let focusHR = false;
    if (["---", "___", "***"].includes(editElement.textContent) && type !== "NodeCodeBlock") {
        html = `<div data-node-id="${id}" data-type="NodeThematicBreak" class="hr"><div></div></div>`;
        // https://github.com/siyuan-note/siyuan/issues/12593
        const nextBlockElement = getNextBlockSibling(blockElement);
        if (nextBlockElement) {
            if (!isNotEditBlock(nextBlockElement)) {
                focusBlock(nextBlockElement);
            } else {
                focusHR = true;
            }
        } else {
            html += genEmptyBlock(false, true);
        }
    } else {
        if (type !== "NodeCodeBlock" && (
            trimStartHTML.startsWith("```") || trimStartHTML.startsWith("~~~") || trimStartHTML.startsWith("···") ||
            (trimStartHTML.indexOf("\n```") > -1 && trimStartText.indexOf("\n```") > -1) ||
            (trimStartHTML.indexOf("\n~~~") > -1 && trimStartText.indexOf("\n~~~") > -1) ||
            (trimStartHTML.indexOf("\n···") > -1 && trimStartText.indexOf("\n···") > -1)
        )) {
            if (trimStartHTML.indexOf("\n") === -1 && trimStartHTML.replace(/·|~/g, "`").replace(/^`{3,}/g, "").indexOf("`") > -1) {
                // ```test` is left alone and renders normally as a paragraph block
            } else {
                let replaceInnerHTML = editElement.innerHTML.trim().replace(/^(~|·|`){3,}/g, "```").replace(/\n(~|·|`){3,}/g, "\n```").trim();
                if (replaceInnerHTML.endsWith("\n```<wbr>") &&
                    (replaceInnerHTML.split("\n```").length - 1 + (replaceInnerHTML.startsWith("```") ? 1 : 0)) % 2 === 0) {
                    // Already-closed matches don't need to be added https://github.com/siyuan-note/siyuan/issues/16053
                } else if (!replaceInnerHTML.endsWith("\n```")) {
                    // The case that needs to be added when ending with "\n```<wbr>" https://github.com/siyuan-note/siyuan/issues/16519
                    replaceInnerHTML = replaceInnerHTML.replace("<wbr>", "") + "<wbr>\n```";
                }
                editElement.innerHTML = replaceInnerHTML;
                html = blockElement.outerHTML;
            }
        }
        // Insert a space between adjacent tags as a separator, to avoid SpinBlockDOM merging them into a single
        // tag during parsing https://github.com/siyuan-note/siyuan/issues/18191
        // Use iterative replacement to handle multiple consecutive adjacent tags (a global regex can't match overlapping cases)
        // If <wbr> (the caret marker) is present in between, it must be preserved after replacement, otherwise
        // focusByWbr can't locate the caret
        let prevHTML: string;
        do {
            prevHTML = html;
            html = html.replace(/(data-type="tag[^"]*">[\s\S]*?<\/span>)((?:\u200b|<wbr>)*)(<span data-type="tag[^"]*">)/, (match, before, between, after) => {
                return before + (between.indexOf("<wbr>") > -1 ? " <wbr>" : " ") + after;
            });
        } while (html !== prevHTML);
        html = protyle.lute.SpinBlockDOM(html);
    }
    // Undoing to the last step in the math formula input box, then continuing to undo, would undo the editor's
    // body content and thereby trigger the input event
    hideElements(["util"], protyle, true);
    if (type === "NodeTable") {
        blockElement.querySelector(".table__select").removeAttribute("style");
    }
    const tempElement = document.createElement("template");
    tempElement.innerHTML = html;
    // The first paragraph block right next to the marker inside a list item isn't allowed to produce a nested list https://github.com/siyuan-note/siyuan/issues/17890
    if (blockElement.closest('[data-type="NodeListItem"]') &&
        blockElement.previousElementSibling?.classList.contains("protyle-action")) {
        if (tempElement.content.firstElementChild.classList.contains("list")) {
            getContenteditableElement(blockElement).innerHTML = "<wbr>";
            html = blockElement.outerHTML;
            tempElement.innerHTML = html;
        }
    }
    if (needRender && (
            getContenteditableElement(tempElement.content.firstElementChild)?.innerHTML !== getContenteditableElement(blockElement).innerHTML ||
            // The caret can't be reached with the up/down arrow keys after the content is deleted https://github.com/siyuan-note/siyuan/issues/4167 https://ld246.com/article/1636256333803
            tempElement.content.childElementCount === 1 && getContenteditableElement(tempElement.content.firstElementChild)?.innerHTML === "<wbr>"
        ) &&
        !(tempElement.content.childElementCount === 1 && tempElement.content.firstElementChild.classList.contains("code-block") && type === "NodeCodeBlock")
    ) {
        if (blockElement.getAttribute("data-type") === "NodeHeading" && blockElement.getAttribute("fold") === "1" &&
            tempElement.content.firstElementChild.getAttribute("data-subtype") !== blockElement.dataset.subtype) {
            setFold(protyle, blockElement, undefined, undefined, false);
            html = html.replace(' fold="1"', "");
            protyle.wysiwyg.lastHTMLs[id] = blockElement.outerHTML;
        }
        let scrollLeft: number;
        let scrollTop: number;
        let contentScrollTop: number;
        if (blockElement.classList.contains("table")) {
            // A table's horizontal/vertical scrolling both happen on the first child node (the contenteditable
            // container with overflow:auto), so it needs to be restored together after rebuilding the DOM,
            // otherwise typing in a long table with a sticky header would jump back to the top
            // https://github.com/siyuan-note/siyuan/issues/18035
            scrollLeft = blockElement.firstElementChild.scrollLeft;
            scrollTop = blockElement.firstElementChild.scrollTop;
            contentScrollTop = protyle.contentElement.scrollTop;
        }
        if (!/<span data-type="backslash">.{1,8}<\/span><wbr>/.test(html)) {
            // Continuing to type after closing via markdown should produce plain text; an escape doesn't need a zwsp added
            html = html.replace("</span><wbr>", "</span>" + Constants.ZWSP + "<wbr>");
        }
        blockElement.insertAdjacentHTML("afterend", html);
        blockElement = blockElement.nextElementSibling as HTMLElement;
        blockElement.previousElementSibling.remove();
        blockElement.setAttribute(Constants.ATTRIBUTE_EDITING, "true");
        // https://github.com/siyuan-note/siyuan/issues/8972
        if (html.split('<span data-type="inline-math" data-subtype="math"').length > 1) {
            Array.from(blockElement.querySelectorAll('[data-type="inline-math"]')).find((item: HTMLElement) => {
                if (item.dataset.content.indexOf("<wbr>") > -1) {
                    item.setAttribute("data-content", item.dataset.content.replace("<wbr>", ""));
                    protyle.toolbar.showRender(protyle, item);
                    return true;
                }
            });
        }
        html = "";
        Array.from(tempElement.content.children).forEach((item, index) => {
            const tempId = item.getAttribute("data-node-id");
            let realElement;
            if (tempId === id) {
                realElement = blockElement;
            } else {
                realElement = protyle.wysiwyg.element.querySelector(`[data-node-id="${tempId}"]`);
            }
            const realType = realElement.getAttribute("data-type");
            let itemHTML = "";
            if (realType === "NodeCodeBlock") {
                const languageElement = realElement.querySelector(".protyle-action__language");
                if (languageElement) {
                    if (window.siyuan.storage[Constants.LOCAL_CODELANG] && languageElement.textContent === "") {
                        languageElement.textContent = window.siyuan.storage[Constants.LOCAL_CODELANG];
                    }
                    highlightRender(realElement);
                } else if (tempElement.content.childElementCount === 1) {
                    protyle.toolbar.showRender(protyle, realElement);
                }
            } else if (["NodeMathBlock", "NodeHTMLBlock"].includes(realType)) {
                if (realType === "NodeMathBlock") {
                    mathRender(realElement);
                }
                protyle.toolbar.showRender(protyle, realElement);
            } else if (realType === "NodeBlockQueryEmbed") {
                blockRender(protyle, realElement);
                if (!realElement.getAttribute("data-content")) {
                    protyle.toolbar.showRender(protyle, realElement);
                }
                hideElements(["hint"], protyle);
            } else if (realType === "NodeThematicBreak" && focusHR) {
                focusBlock(blockElement);
            } else {
                // https://github.com/siyuan-note/siyuan/issues/6087
                realElement.querySelectorAll('[data-type~="block-ref"][data-subtype="d"]').forEach(refItem => {
                    if (refItem.textContent === "") {
                        fetchPost("/api/block/getRefText", {id: refItem.getAttribute("data-id")}, (response) => {
                            refItem.innerHTML = response.data;
                        });
                    }
                });
                mathRender(realElement);
                if (index === tempElement.content.childElementCount - 1) {
                    // https://github.com/siyuan-note/siyuan/issues/11156
                    const currentWbrElement = blockElement.querySelector("wbr");
                    if (currentWbrElement && currentWbrElement.parentElement.tagName === "SPAN" && currentWbrElement.parentElement.innerHTML === "<wbr>") {
                        const types = currentWbrElement.parentElement.getAttribute("data-type") || "";
                        if (types.includes("sup") || types.includes("u") || types.includes("sub")) {
                            currentWbrElement.insertAdjacentText("beforebegin", Constants.ZWSP);
                        }
                    }
                    itemHTML = realElement.outerHTML;
                    focusByWbr(protyle.wysiwyg.element, range);
                    protyle.hint.render(protyle);
                    // When a table has a scrollbar, typing a digit scrolls it forward https://github.com/siyuan-note/siyuan/issues/3650
                    if (scrollLeft > 0) {
                        blockElement.firstElementChild.scrollLeft = scrollLeft;
                    }
                    if (scrollTop > 0) {
                        blockElement.firstElementChild.scrollTop = scrollTop;
                    }
                    // SpinBlockDOM generates a new table and replaces the old node; removing the old node
                    // invalidates the outer editor's scroll anchor, so the scroll position needs to be restored
                    // after the caret is restored
                    // https://github.com/siyuan-note/siyuan/issues/18235
                    if (contentScrollTop > 0) {
                        protyle.contentElement.scrollTop = contentScrollTop;
                        protyle.scroll.lastScrollTop = contentScrollTop - 1;
                    }
                }
            }
            // https://github.com/siyuan-note/siyuan/issues/14766
            html += itemHTML || realElement.outerHTML;
        });
    } else if (blockElement.getAttribute("data-type") === "NodeCodeBlock") {
        editElement.parentElement.removeAttribute("data-render");
        highlightRender(blockElement);
    } else {
        focusByWbr(protyle.wysiwyg.element, range);
        protyle.hint.render(protyle);
    }
    hideElements(["gutter"], protyle);
    updateInput(html, protyle, id);
};

const updateInput = (html: string, protyle: IProtyle, id: string) => {
    const tempElement = document.createElement("template");
    tempElement.innerHTML = html;
    const doOperations: IOperation[] = [];
    const undoOperations: IOperation[] = [];
    Array.from(tempElement.content.children).forEach((item, index) => {
        if (item.getAttribute("spellcheck") === "false" && item.getAttribute("contenteditable") === "false") {
            item.setAttribute("contenteditable", "true");
        }
        const tempId = item.getAttribute("data-node-id");
        if (tempId === id) {
            doOperations.push({
                id,
                data: item.outerHTML,
                action: "update"
            });
            undoOperations.push({
                id,
                data: protyle.wysiwyg.lastHTMLs[id],
                action: "update"
            });
            protyle.wysiwyg.lastHTMLs[id] = item.outerHTML;
        } else {
            let firstElement;
            if (index === 0) {
                firstElement = protyle.wysiwyg.element.querySelector(`[data-node-id="${tempId}"]`);
            }
            doOperations.push({
                action: "insert",
                data: item.outerHTML,
                id: tempId,
                previousID: index === 0 ? (firstElement ? getPreviousBlockSibling(firstElement)?.getAttribute("data-node-id") : undefined) : item.previousElementSibling.getAttribute("data-node-id"),
                parentID: firstElement ? (getParentBlock(firstElement).getAttribute("data-node-id") || protyle.block.parentID) : protyle.block.parentID
            });
            undoOperations.push({
                id: tempId,
                action: "delete"
            });
        }
    });
    transaction(protyle, doOperations, undoOperations);
};
