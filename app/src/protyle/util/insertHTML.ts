import {hasClosestBlock, hasClosestByAttribute, hasClosestByClassName, hasClosestByTag} from "./hasClosest";
import * as dayjs from "dayjs";
import {transaction, updateTransaction} from "../wysiwyg/transaction";
import {fixAdjacentTags, getContenteditableElement, getParentBlock, getPreviousBlockSibling} from "../wysiwyg/getBlock";
import {
    fixTableRange,
    focusBlock,
    focusByRange,
    focusByWbr,
    getEditorRange,
    getSelectionOffset,
    setLastNodeRange,
} from "./selection";
import {Constants} from "../../constants";
import {highlightRender} from "../render/highlightRender";
import {scrollCenter} from "../../util/highlightById";
import {updateAttrViewCellAnimation, updateAVName} from "../render/av/action";
import {updateCellsValue} from "../render/av/cell";
import {input} from "../wysiwyg/input";
import {updateListOrder} from "../wysiwyg/list";
import {fetchPost} from "../../util/fetch";
import {getTableRangeCells, isIncludeCell} from "./table";
import {getFieldIdByCellElement, getRowHTML} from "../render/av/row";
import {getAvBodyData} from "../render/av/virtualScroll";
import {processClonePHElement} from "../render/util";
import {setFold} from "./blockFold";

// Marks a placeholder row temporarily inserted during paste; removed altogether once the traversal ends, to
// avoid polluting virtual scrolling's renderedStart/renderedEnd/spacer state
const PLACEHOLDER_ROW_CLASS = "av__row--placeholder";

// Get the row after the current data row. Virtual scrolling trims data rows outside the viewport, in which
// case nextElementSibling points to a non-data row such as .av__row--util. This increments by data-index, and
// if the target row hasn't been rendered, generates a placeholder row from the data source and inserts it
// before returning, so paste can cover data rows outside the viewport (trimmed by virtual scrolling). Returns
// null once nextIndex exceeds the existing row count, to end the traversal.
// The placeholder row is marked with av__row--placeholder, and the caller removes it after the paste loop
// ends, so virtual scrolling state isn't corrupted.
const getNextDataRow = (currentRowElement: Element): HTMLElement => {
    const nextSibling = currentRowElement.nextElementSibling as HTMLElement;
    if (nextSibling && nextSibling.classList.contains("av__row") &&
        !nextSibling.classList.contains("av__row--util") &&
        !nextSibling.classList.contains("av__row--footer") &&
        !nextSibling.classList.contains("av__row--header")) {
        return nextSibling;
    }
    const nextIndex = parseInt(currentRowElement.getAttribute("data-index")) + 1;
    const bodyElement = hasClosestByClassName(currentRowElement, "av__body") as HTMLElement;
    if (!bodyElement) {
        return null;
    }
    const view = getAvBodyData(bodyElement) as IAVTable;
    if (!view || !view.rows || nextIndex >= view.rows.length) {
        return null;
    }
    const pinIndex = parseInt(bodyElement.querySelector(".av__row--header > .block__icons")?.getAttribute("data-pinindex") || "-1");
    const rowHTML = getRowHTML({data: view, row: view.rows[nextIndex], rowIndex: nextIndex, pinIndex, type: "table"});
    const bottomElement = bodyElement.querySelector(".av__row--util");
    bottomElement.insertAdjacentHTML("beforebegin", rowHTML);
    const newRowElement = bottomElement.previousElementSibling as HTMLElement;
    newRowElement.classList.add(PLACEHOLDER_ROW_CLASS);
    return newRowElement;
};

// Remove placeholder rows inserted during paste, restoring the DOM to a trimmed state consistent with virtual
// scrolling's bodyStates
const removePlaceholderRows = (blockElement: HTMLElement) => {
    blockElement.querySelectorAll("." + PLACEHOLDER_ROW_CLASS).forEach(item => item.remove());
};

const processAV = (range: Range, html: string, protyle: IProtyle, blockElement: HTMLElement) => {
    const tempElement = document.createElement("template");
    tempElement.innerHTML = html;
    let values: string[][] = [];
    if (html.endsWith("]") && html.startsWith("[")) {
        try {
            values = JSON.parse(html);
        } catch (e) {
            console.warn("insert cell: JSON.parse error");
        }
    } else if (tempElement.content.querySelector("table")) {
        tempElement.content.querySelectorAll("tr").forEach(item => {
            values.push([]);
            Array.from(item.children).forEach(cell => {
                values[values.length - 1].push(cell.textContent);
            });
        });
    }
    const avID = blockElement.dataset.avId;
    fetchPost("/api/av/getAttributeViewKeysByAvID", {avID}, async (response) => {
        const columns: IAVColumn[] = response.data;
        const cellElements: HTMLElement[] = Array.from(blockElement.querySelectorAll(".av__cell--active, .av__cell--select")) || [];
        if (values && Array.isArray(values) && values.length > 0) {
            if (cellElements.length === 0) {
                blockElement.querySelectorAll(".av__row--select:not(.av__row--header)").forEach(rowElement => {
                    rowElement.querySelectorAll(".av__cell").forEach((cellElement: HTMLElement) => {
                        cellElements.push(cellElement);
                    });
                });
            }
            if (cellElements.length === 0) {
                cellElements.push(blockElement.querySelector(".av__row:not(.av__row--header) .av__cell"));
            }
            const doOperations: IOperation[] = [];
            const undoOperations: IOperation[] = [];

            const id = blockElement.dataset.nodeId;
            let currentRowElement: Element;
            const firstColIndex = cellElements[0].getAttribute("data-col-id");
            for (let i = 0; i < values.length; i++) {
                if (!currentRowElement) {
                    currentRowElement = hasClosestByClassName(cellElements[0].parentElement, "av__row") as HTMLElement;
                } else {
                    currentRowElement = getNextDataRow(currentRowElement);
                }
                if (!currentRowElement) {
                    break;
                }
                let cellElement: HTMLElement;
                for (let j = 0; j < values[i].length; j++) {
                    const cellValue = values[i][j];
                    if (!cellElement) {
                        cellElement = currentRowElement.querySelector(`.av__cell[data-col-id="${firstColIndex}"]`) as HTMLElement;
                    } else {
                        if (cellElement.nextElementSibling) {
                            cellElement = cellElement.nextElementSibling as HTMLElement;
                        } else if (cellElement.parentElement.classList.contains("av__colsticky")) {
                            cellElement = cellElement.parentElement.nextElementSibling as HTMLElement;
                        }
                    }
                    if (!cellElement.classList.contains("av__cell")) {
                        break;
                    }
                    const operations = await updateCellsValue(protyle, blockElement as HTMLElement,
                        cellValue, [cellElement], columns, html, true);
                    if (operations.doOperations.length > 0) {
                        doOperations.push(...operations.doOperations);
                        undoOperations.push(...operations.undoOperations);
                    }
                }
            }
            removePlaceholderRows(blockElement as HTMLElement);
            if (doOperations.length > 0) {
                doOperations.push({
                    action: "doUpdateUpdated",
                    id,
                    data: dayjs().format("YYYYMMDDHHmmss"),
                });
                undoOperations.push({
                    action: "doUpdateUpdated",
                    id,
                    data: blockElement.getAttribute("updated"),
                });
                transaction(protyle, doOperations, undoOperations);
            }
            return;
        }

        const contenteditableElement = getContenteditableElement(tempElement.content.firstElementChild);
        if (contenteditableElement && contenteditableElement.childNodes.length === 1 && contenteditableElement.firstElementChild?.getAttribute("data-type") === "block-ref") {
            const selectCellElement = blockElement.querySelector(".av__cell--select") as HTMLElement;
            if (selectCellElement) {
                const sourceId = contenteditableElement.firstElementChild.getAttribute("data-id");
                const previousID = getFieldIdByCellElement(selectCellElement, blockElement.getAttribute("data-av-type") as TAVView);
                transaction(protyle, [{
                    action: "replaceAttrViewBlock",
                    avID,
                    previousID,
                    nextID: sourceId,
                    isDetached: false,
                }], [{
                    action: "replaceAttrViewBlock",
                    avID,
                    previousID: sourceId,
                    nextID: previousID,
                    isDetached: selectCellElement.dataset.detached === "true",
                }]);
                updateAttrViewCellAnimation(selectCellElement, {
                    type: "block",
                    isDetached: false,
                    block: {content: contenteditableElement.firstElementChild.textContent, id: sourceId}
                });
                return;
            }
        }

        const text = protyle.lute.BlockDOM2Content(html);
        const rowsElement = blockElement.querySelectorAll(".av__row--select");

        const textJSON: string[][] = [];
        text.split("\n").forEach(row => {
            textJSON.push(row.split("\t"));
        });
        if (rowsElement.length > 0 && textJSON.length === 1 && textJSON[0].length === 1) {
            updateCellsValue(protyle, blockElement as HTMLElement, text, undefined, columns, html);
            return;
        }
        if (rowsElement.length > 0) {
            rowsElement.forEach(rowElement => {
                rowElement.querySelectorAll(".av__cell").forEach((cellElement: HTMLElement) => {
                    cellElements.push(cellElement);
                });
            });
        }
        if (cellElements.length > 0) {
            if (textJSON.length === 1 && textJSON[0].length === 1) {
                updateCellsValue(protyle, blockElement as HTMLElement, text, cellElements, columns, html);
            } else {
                let currentRowElement: Element;
                const doOperations: IOperation[] = [];
                const undoOperations: IOperation[] = [];
                const firstColIndex = cellElements[0].getAttribute("data-col-id");
                for (let i = 0; i < textJSON.length; i++) {
                    if (!currentRowElement) {
                        currentRowElement = hasClosestByClassName(cellElements[0].parentElement, "av__row") as HTMLElement;
                    } else {
                        currentRowElement = getNextDataRow(currentRowElement);
                    }
                    if (!currentRowElement) {
                        break;
                    }
                    let cellElement: HTMLElement;
                    for (let j = 0; j < textJSON[i].length; j++) {
                        if (!cellElement) {
                            cellElement = currentRowElement.querySelector(`.av__cell[data-col-id="${firstColIndex}"]`) as HTMLElement;
                        } else {
                            if (cellElement.nextElementSibling) {
                                cellElement = cellElement.nextElementSibling as HTMLElement;
                            } else if (cellElement.parentElement.classList.contains("av__colsticky")) {
                                cellElement = cellElement.parentElement.nextElementSibling as HTMLElement;
                            }
                        }
                        if (!cellElement.classList.contains("av__cell")) {
                            break;
                        }
                        const cellValue = textJSON[i][j];
                        const operations = await updateCellsValue(protyle, blockElement as HTMLElement, cellValue, [cellElement], columns,
                            cellElement.getAttribute("data-dtype") === "mAsset" ? (tempElement.content.children[i * (j + 1) + j]?.outerHTML || "") : html, true);
                        if (operations.doOperations.length > 0) {
                            doOperations.push(...operations.doOperations);
                            undoOperations.push(...operations.undoOperations);
                        }
                    }
                }
                removePlaceholderRows(blockElement as HTMLElement);
                if (doOperations.length > 0) {
                    const id = blockElement.getAttribute("data-node-id");
                    doOperations.push({
                        action: "doUpdateUpdated",
                        id,
                        data: dayjs().format("YYYYMMDDHHmmss"),
                    });
                    undoOperations.push({
                        action: "doUpdateUpdated",
                        id,
                        data: blockElement.getAttribute("updated"),
                    });
                    transaction(protyle, doOperations, undoOperations);
                }
            }
            document.querySelector(".av__panel")?.remove();
        } else if (hasClosestByClassName(range.startContainer, "av__title")) {
            const node = document.createTextNode(text);
            range.insertNode(node);
            range.setEnd(node, text.length);
            range.collapse(false);
            focusByRange(range);
            updateAVName(protyle, blockElement);
        }
    });
};

interface ITablePasteRange {
    table: HTMLTableElement;
    startCell: HTMLTableCellElement;
    endCell: HTMLTableCellElement;
}

const getTablePasteRange = (range: Range): ITablePasteRange | undefined => {
    if (range.collapsed) {
        return undefined;
    }
    const startCell = (hasClosestByTag(range.startContainer, "TD") ||
        hasClosestByTag(range.startContainer, "TH")) as HTMLTableCellElement;
    const endCell = (hasClosestByTag(range.endContainer, "TD") ||
        hasClosestByTag(range.endContainer, "TH")) as HTMLTableCellElement;
    if (!startCell || !endCell || startCell === endCell) {
        return undefined;
    }
    const table = startCell.closest("table");
    if (!table || table !== endCell.closest("table")) {
        return undefined;
    }
    return {table, startCell, endCell};
};

const processTable = (range: Range, html: string, protyle: IProtyle, blockElement: HTMLElement,
                      pasteRange?: ITablePasteRange) => {
    const tempElement = document.createElement("template");
    tempElement.innerHTML = html;
    const copyTableElement = tempElement.content.querySelector("table") as HTMLTableElement;
    if (!copyTableElement) {
        return false;
    }
    const copyCells = getTableRangeCells(copyTableElement);
    if (copyCells.length === 0) {
        return false;
    }
    const tableElement = blockElement.querySelector("table") as HTMLTableElement;
    const scrollLeft = blockElement.firstElementChild.scrollLeft;
    const scrollTop = tableElement.scrollTop;
    const tableSelectElement = blockElement.querySelector(".table__select") as HTMLElement;
    let startCell = pasteRange?.table === tableElement ? pasteRange.startCell : undefined;
    let endCell = pasteRange?.table === tableElement ? pasteRange.endCell : undefined;
    if (!startCell || !endCell) {
        tableElement.querySelectorAll("th, td").forEach((item: HTMLTableCellElement) => {
            if (!item.classList.contains("fn__none") && isIncludeCell({
                tableSelectElement,
                scrollLeft,
                scrollTop,
                item,
            })) {
                if (!startCell) {
                    startCell = item;
                }
                endCell = item;
            }
        });
    }
    if (!startCell || !endCell) {
        return false;
    }
    const targetCells = getTableRangeCells(tableElement, startCell, endCell);
    // Match actual cells by their logical grid coordinates, to avoid content misalignment caused by colspan/rowspan fn__none placeholders.
    const copyCellMap = new Map(copyCells.map(item => [`${item.row}:${item.col}`, item.cell]));
    const matchedCells: { source: HTMLTableCellElement; target: HTMLTableCellElement }[] = [];
    targetCells.forEach(item => {
        const source = copyCellMap.get(`${item.row}:${item.col}`);
        if (source) {
            matchedCells.push({source, target: item.cell});
        }
    });
    if (matchedCells.length === 0) {
        return false;
    }
    tableSelectElement.removeAttribute("style");
    const oldHTML = blockElement.outerHTML;
    blockElement.setAttribute("updated", dayjs().format("YYYYMMDDHHmmss"));
    matchedCells.forEach((item, index) => {
        item.target.innerHTML = item.source.innerHTML;
        if (index === matchedCells.length - 1) {
            setLastNodeRange(item.target, range, false);
        }
    });
    range.collapse(false);
    updateTransaction(protyle, blockElement, oldHTML);
    return true;
};

export const insertHTML = (html: string, protyle: IProtyle, isBlock = false,
                           // On mobile, when inserting an embed block the range obtained is a stale value
                           useProtyleRange = false,
                           // Pasting a block at the very beginning inserts it above
                           insertByCursor = false) => {
    if (html === "") {
        return;
    }
    const range = useProtyleRange ? protyle.toolbar.range : getEditorRange(protyle.wysiwyg.element);
    const tablePasteRange = getTablePasteRange(range);
    fixTableRange(range);
    let unSpinHTML;
    if (hasClosestByAttribute(range.startContainer, "data-type", "NodeTable") && !isBlock) {
        if (hasClosestByTag(range.startContainer, "TABLE")) {
            unSpinHTML = protyle.lute.BlockDOM2InlineBlockDOM(html);
        } else {
            // https://github.com/siyuan-note/siyuan/issues/9411
            isBlock = true;
        }
    }
    let blockElement = hasClosestBlock(range.startContainer) as HTMLElement;
    if (!blockElement) {
        // The range is lost after using a mouse click to select from the template hint list
        if (protyle.toolbar.range) {
            blockElement = hasClosestBlock(protyle.toolbar.range.startContainer) as HTMLElement;
        } else {
            blockElement = protyle.wysiwyg.element.firstElementChild as HTMLElement;
        }
    }
    if (!blockElement) {
        return;
    }

    if (blockElement.classList.contains("av")) {
        const avTitleElement = hasClosestByClassName(range.startContainer, "av__title");
        if (!avTitleElement || (avTitleElement && !isBlock)) {
            range.deleteContents();
            processAV(range, html, protyle, blockElement as HTMLElement);
            return;
        }
    }
    if (blockElement.classList.contains("table") &&
        (tablePasteRange || blockElement.querySelector(".table__select").clientWidth > 0) &&
        processTable(range, html, protyle, blockElement, tablePasteRange)) {
        return;
    }

    let id = blockElement.getAttribute("data-node-id");
    range.insertNode(document.createElement("wbr"));
    let oldHTML = blockElement.outerHTML;
    const type = blockElement.getAttribute("data-type");
    const isNodeCodeBlock = type === "NodeCodeBlock";
    const editableElement = getContenteditableElement(blockElement);
    if (!isBlock &&
        (isNodeCodeBlock || protyle.toolbar.getCurrentType(range).includes("code"))) {
        range.deleteContents();
        // A code block must keep at least one \n https://github.com/siyuan-note/siyuan/pull/13271#issuecomment-2502672155
        let codeBlockIsEmpty = false;
        if (isNodeCodeBlock && editableElement.textContent === "") {
            codeBlockIsEmpty = true;
        }
        range.insertNode(document.createTextNode(html.replace(/\r\n|\r|\u2028|\u2029/g, "\n")));
        range.collapse(false);
        range.insertNode(document.createElement("wbr"));
        if (codeBlockIsEmpty) {
            // The \n added for an empty code block must go at the very end https://github.com/siyuan-note/siyuan/issues/15399
            range.collapse(false);
            range.insertNode(document.createTextNode("\n"));
        }
        if (isNodeCodeBlock) {
            blockElement.querySelector('[data-render="true"]')?.removeAttribute("data-render");
            highlightRender(blockElement);
        } else {
            focusByWbr(blockElement, range);
        }
        blockElement.setAttribute("updated", dayjs().format("YYYYMMDDHHmmss"));
        updateTransaction(protyle, blockElement, oldHTML);
        setTimeout(() => {
            scrollCenter(protyle, undefined, "nearest", "smooth");
        }, Constants.TIMEOUT_LOAD);
        return;
    }

    const undoOperation: IOperation[] = [];
    const doOperation: IOperation[] = [];
    if (range.toString() !== "") {
        const inlineMathElement = hasClosestByAttribute(range.commonAncestorContainer, "data-type", "inline-math");
        if (inlineMathElement) {
            // Selecting a math formula inside a table https://ld246.com/article/1631708573504
            inlineMathElement.remove();
        } else if (range.startContainer.nodeType === 3 && range.startContainer.parentElement.getAttribute("data-type")?.indexOf("block-ref") > -1) {
            // Alt+[ after selecting ref**bbb**
            range.deleteContents();
            // https://github.com/siyuan-note/siyuan/issues/14035
            if (range.startContainer.nodeType !== 3 && (range.startContainer as Element).tagName === "SPAN" &&
                range.startContainer.textContent === "") {
                // Handling a selected ref https://ld246.com/article/1629214377537
                (range.startContainer as HTMLElement).remove();
            }
        } else {
            range.deleteContents();
        }
        range.insertNode(document.createElement("wbr"));
        blockElement.setAttribute(Constants.ATTRIBUTE_EDITING, "true");
        undoOperation.push({
            action: "update",
            id,
            data: oldHTML
        });
        doOperation.push({
            action: "update",
            id,
            data: blockElement.outerHTML
        });
    }
    const tempElement = document.createElement("template");

    // https://github.com/siyuan-note/siyuan/issues/14162 & https://github.com/siyuan-note/siyuan/issues/14965
    if (/^\s*&gt;|\*|-|\+|\d*.|\[ \]|[x]/.test(html) &&
        editableElement.textContent.replace(Constants.ZWSP, "") !== "") {
        unSpinHTML = html;
    }

    let innerHTML = unSpinHTML || // Inserting into a table requires the already-converted inline elements https://github.com/siyuan-note/siyuan/issues/9358
        html;   // Spin no longer preserves spaces, so the original text must be used
    // Pasting plain text internally escapes it, so it needs to be unescaped here https://github.com/siyuan-note/siyuan/issues/10620
    innerHTML = innerHTML.replace(/;;;lt;;;/g, "&lt;").replace(/;;;gt;;;/g, "&gt;");
    tempElement.innerHTML = innerHTML;

    let block2text = false;
    if ((
            editableElement.textContent.replace(Constants.ZWSP, "") !== "" ||
            type === "NodeHeading"
        ) &&
        tempElement.content.childElementCount === 1 &&
        tempElement.content.firstChild.nodeType !== 3 &&
        tempElement.content.firstElementChild.getAttribute("data-type") === "NodeHeading") {
        // https://github.com/siyuan-note/siyuan/issues/14114
        isBlock = false;
        block2text = true;
    }
    // Using a lute method adds a p element; only copy the inner content when there's a single p element, a
    // single string, or something like <u>b</u>
    if (!isBlock) {
        if (tempElement.content.firstChild.nodeType === 3 || block2text ||
            (tempElement.content.firstChild.nodeType !== 3 &&
                ((tempElement.content.firstElementChild.classList.contains("p") && tempElement.content.childElementCount === 1) ||
                    tempElement.content.firstElementChild.tagName !== "DIV"))) {
            if (tempElement.content.firstChild.nodeType !== 3 && tempElement.content.firstElementChild.classList.contains("p")) {
                tempElement.innerHTML = tempElement.content.firstElementChild.firstElementChild.innerHTML.trim();
            }
            // Pasting a styled inline element into another inline element requires splitting it
            const spanElement = range.startContainer.nodeType === 3 ? range.startContainer.parentElement : range.startContainer as HTMLElement;
            const splitElements: HTMLElement[] = [];
            if (spanElement.tagName === "SPAN" && spanElement === (range.endContainer.nodeType === 3 ? range.endContainer.parentElement : range.endContainer) &&
                // Pasting plain text doesn't need splitting https://ld246.com/article/1665556907936
                // An emoji image needs splitting https://github.com/siyuan-note/siyuan/issues/9370
                tempElement.content.querySelector("span, img")
            ) {
                const afterElement = document.createElement("span");
                const attributes = spanElement.attributes;
                for (let i = 0; i < attributes.length; i++) {
                    afterElement.setAttribute(attributes[i].name, attributes[i].value);
                }
                range.setEnd(spanElement.lastChild, spanElement.lastChild.textContent.length);
                afterElement.append(range.extractContents());
                spanElement.after(afterElement);
                range.setStartBefore(afterElement);
                range.collapse(true);
                splitElements.push(spanElement, afterElement);
            }
            range.insertNode(tempElement.content.cloneNode(true));
            range.collapse(false);
            blockElement.querySelector("wbr")?.remove();
            // Remove the empty split element produced when inserting at an inline element's boundary, so
            // adjacent-tag repair doesn't insert a space after the new tag
            splitElements.forEach((item) => {
                if (item.childElementCount === 0 && item.textContent.split(Constants.ZWSP).join("") === "") {
                    item.remove();
                }
            });
            // Insert a space between adjacent tags as a separator, so a later SpinBlockDOM parse doesn't merge
            // them into a single tag https://github.com/siyuan-note/siyuan/issues/18191
            fixAdjacentTags(getContenteditableElement(blockElement));
            protyle.wysiwyg.lastHTMLs[id] = oldHTML;
            input(protyle, blockElement as HTMLElement, range);
            return;
        }
    }
    // Whether the caret is in a list item's first paragraph block (right next to protyle-action)
    const isFirstBlockInLi = hasClosestByClassName(blockElement, "li") &&
        blockElement.previousElementSibling?.classList.contains("protyle-action");
    const cursorLiElement = hasClosestByClassName(blockElement, "li");
    // Unify the list type when pasting a list into an existing list https://github.com/siyuan-note/siyuan/issues/17890
    if (cursorLiElement) {
        const targetSubtype = cursorLiElement.getAttribute("data-subtype");
        const firstChild = tempElement.content.firstElementChild;
        if (firstChild && (firstChild.getAttribute("data-type") === "NodeList" ||
            firstChild.getAttribute("data-type") === "NodeListItem") &&
            firstChild.getAttribute("data-subtype") !== targetSubtype) {
            tempElement.content.querySelectorAll(".li").forEach(li => {
                li.setAttribute("data-subtype", targetSubtype);
                const actionElement = li.querySelector(".protyle-action");
                if (!actionElement) return;
                if (targetSubtype === "o") {
                    li.removeAttribute("data-task");
                    li.setAttribute("data-marker", "1.");
                    actionElement.className = "protyle-action protyle-action--order";
                    actionElement.setAttribute("contenteditable", "false");
                    actionElement.textContent = "1.";
                } else if (targetSubtype === "t") {
                    li.setAttribute("data-marker", "*");
                    li.setAttribute("data-task", " ");
                    actionElement.className = "protyle-action protyle-action--task";
                    actionElement.removeAttribute("contenteditable");
                    actionElement.innerHTML = "<svg><use xlink:href=\"#iconUncheck\"></use></svg>";
                } else {
                    li.removeAttribute("data-task");
                    li.setAttribute("data-marker", "*");
                    actionElement.className = "protyle-action";
                    actionElement.removeAttribute("contenteditable");
                    actionElement.innerHTML = "<svg><use xlink:href=\"#iconDot\"></use></svg>";
                }
            });
            tempElement.content.querySelectorAll("[data-type='NodeList']").forEach(list => {
                list.setAttribute("data-subtype", targetSubtype);
            });
        }
    }
    let isListPaste = false;
    let keepEmptyBlock = false;
    // A list item can't be pasted on its own https://ld246.com/article/1628681120576/comment/1628681209731#comments
    if (tempElement.content.children[0]?.getAttribute("data-type") === "NodeListItem") {
        isListPaste = true;
        if (cursorLiElement) {
            blockElement = cursorLiElement;
            id = blockElement.getAttribute("data-node-id");
            oldHTML = blockElement.outerHTML;
        } else {
            const liItemElement = tempElement.content.children[0];
            const subType = liItemElement.getAttribute("data-subtype");
            tempElement.innerHTML = `<div${subType === "o" ? " data-marker=\"1.\"" : ""} data-subtype="${subType}" data-node-id="${Lute.NewNodeID()}" data-type="NodeList" class="list">${html}<div class="protyle-attr" contenteditable="false">${Constants.ZWSP}</div></div>`;
        }
    } else if (isFirstBlockInLi && cursorLiElement &&
        tempElement.content.children[0]?.getAttribute("data-type") === "NodeList") {
        const sourceList = tempElement.content.children[0] as HTMLElement;
        const hasRefCount = sourceList.querySelector(".protyle-attr--refcount");
        if (!hasRefCount) {
            isListPaste = true;
            // Pasting a list block into a top-level empty list item splits it into sibling list items https://github.com/siyuan-note/siyuan/issues/17890
            blockElement = cursorLiElement as HTMLElement;
            id = blockElement.getAttribute("data-node-id");
            oldHTML = blockElement.outerHTML;
            const listElement = tempElement.content.children[0] as HTMLElement;
            tempElement.innerHTML = "";
            while (listElement.firstElementChild) {
                if (listElement.firstElementChild.classList.contains("protyle-attr")) {
                    listElement.firstElementChild.remove();
                    continue;
                }
                tempElement.content.appendChild(listElement.firstElementChild);
            }
        } else {
            // A list with a refcount is inserted directly as a nested list after the empty paragraph, without
            // splitting or cleanup https://github.com/siyuan-note/siyuan/issues/17890
            keepEmptyBlock = true;
        }
    }
    let lastElement: Element;
    let insertBefore = false;
    if (!range.toString() && insertByCursor) {
        const positon = getSelectionOffset(blockElement, protyle.wysiwyg.element, range);
        if (positon.start === 0 && editableElement.textContent !== "") {
            insertBefore = true;
        }
    }
    // https://github.com/siyuan-note/siyuan/issues/15768
    if (tempElement.content.firstChild.nodeType === 3 || (tempElement.content.firstChild.nodeType === 1 && tempElement.content.firstElementChild.tagName !== "DIV")) {
        tempElement.innerHTML = protyle.lute.SpinBlockDOM(tempElement.innerHTML);
    }
    (insertBefore ? Array.from(tempElement.content.children) : Array.from(tempElement.content.children).reverse()).find((item) => {
        let addId = item.getAttribute("data-node-id");
        const hasParentHeading = item.getAttribute("parent-heading");
        if (addId === id) {
            item.setAttribute(Constants.ATTRIBUTE_EDITING, "true");
            doOperation.push({
                action: "update",
                data: item.outerHTML,
                id: addId,
            });
            undoOperation.push({
                action: "update",
                id: addId,
                data: oldHTML,
            });
        } else {
            if (item.classList.contains("li") && !blockElement.parentElement.classList.contains("list")) {
                // https://github.com/siyuan-note/siyuan/issues/6534
                addId = Lute.NewNodeID();
                const liElement = document.createElement("div");
                liElement.setAttribute("data-subtype", item.getAttribute("data-subtype"));
                liElement.setAttribute("data-node-id", addId);
                liElement.setAttribute("data-type", "NodeList");
                liElement.setAttribute("updated", dayjs().format("YYYYMMDDHHmmss"));
                liElement.classList.add("list");
                liElement.append(item);
                item = liElement;
            }
            item.removeAttribute("parent-heading");
            doOperation.push({
                action: "insert",
                data: item.outerHTML,
                id: addId,
                context: {ignoreProcess: hasParentHeading ? "true" : "false"},
                nextID: insertBefore ? id : undefined,
                previousID: insertBefore ? undefined : id
            });
            undoOperation.push({
                action: "delete",
                id: addId,
            });
        }
        if (!hasParentHeading) {
            const rendersElement = [];
            if (item.classList.contains("render-node") && item.getAttribute("data-type") === "NodeCodeBlock") {
                rendersElement.push(item);
            } else {
                rendersElement.push(...item.querySelectorAll('.render-node[data-type="NodeCodeBlock"]'));
            }
            rendersElement.forEach((renderItem) => {
                renderItem.querySelector(".protyle-icons")?.remove();
                const spinElement = renderItem.querySelector('[spin="1"]');
                if (spinElement) {
                    spinElement.innerHTML = "";
                }
                renderItem.removeAttribute("data-render");
            });
            processClonePHElement(item);
            if (insertBefore) {
                blockElement.before(item);
            } else {
                blockElement.after(item);
            }
        }
        if (!lastElement) {
            lastElement = item;
        }
    });
    if (editableElement && editableElement.textContent === "" && blockElement.classList.contains("p") && !keepEmptyBlock) {
        // Selecting all content in the current block, pasting, then undoing causes an anomaly https://ld246.com/article/1662542137636
        doOperation.find((item, index) => {
            if (item.id === id) {
                doOperation.splice(index, 1);
                return true;
            }
        });
        doOperation.push({
            action: "delete",
            id
        });
        // Selecting all content in the current block, pasting, then undoing causes an anomaly https://ld246.com/article/1662542137636
        undoOperation.find((item, index) => {
            if (item.id === id && item.action === "update") {
                undoOperation.splice(index, 1);
                return true;
            }
        });
        undoOperation.push({
            action: "insert",
            data: oldHTML,
            id,
            previousID: getPreviousBlockSibling(blockElement)?.getAttribute("data-node-id") || "",
            parentID: getParentBlock(blockElement).getAttribute("data-node-id") || protyle.block.parentID
        });
        blockElement.remove();
    }
    if (lastElement) {
        // https://github.com/siyuan-note/siyuan/issues/5591
        focusBlock(lastElement, undefined, false);
    }
    protyle.wysiwyg.element.querySelectorAll("wbr").forEach(item => {
        item.remove();
    });
    // A copied container block contains a fold heading block
    protyle.wysiwyg.element.querySelectorAll("[parent-heading]").forEach(item => {
        item.remove();
    });
    let foldData;
    if (blockElement.getAttribute("data-type") === "NodeHeading" &&
        blockElement.getAttribute("fold") === "1" && !insertBefore) {
        fetchPost("/api/block/getHeadingChildrenIDs", {id: blockElement.getAttribute("data-node-id")}, (response) => {
            const childrenIDs: string[] = response.data;
            const previousId = (childrenIDs && childrenIDs.length > 0) ? childrenIDs[childrenIDs.length - 1] : blockElement.getAttribute("data-node-id");
            foldData = setFold(protyle, blockElement, true, false, false, true);
            foldData.doOperations[0].context = {
                focusId: lastElement?.getAttribute("data-node-id"),
            };
            doOperation.forEach(item => {
                if (item.action === "insert") {
                    item.previousID = previousId;
                }
            });
            doOperation.splice(0, 0, ...foldData.doOperations);
            undoOperation.push(...foldData.undoOperations);
            transaction(protyle, doOperation, undoOperation);
        });
        return;
    }
    // After pasting into an empty list item (whose first paragraph is empty), delete the empty list item https://github.com/siyuan-note/siyuan/issues/17890
    if (isListPaste && cursorLiElement && isFirstBlockInLi) {
        const editEl = getContenteditableElement(cursorLiElement);
        if (editEl && editEl.textContent.replace(Constants.ZWSP, "").trim() === "") {
            // Move the empty list item's nested list under the last pasted item
            const subList = cursorLiElement.querySelector(":scope > [data-type='NodeList']");
            if (subList && lastElement && lastElement.classList.contains("li")) {
                const movedList = subList.cloneNode(true) as HTMLElement;
                const existSubList = lastElement.querySelector(":scope > [data-type='NodeList']");
                if (existSubList) {
                    // The last item already has a nested list, so merge the list items into it
                    Array.from(movedList.querySelectorAll(":scope > .li")).forEach(li => {
                        existSubList.appendChild(li);
                    });
                } else {
                    lastElement.appendChild(movedList);
                }
                // Update the data of the insert operation for the last item
                const lastUpdateOp = doOperation.find(op => op.action === "insert" && op.id === lastElement.getAttribute("data-node-id"));
                if (lastUpdateOp) {
                    lastUpdateOp.data = lastElement.outerHTML;
                }
            }
            const liId = cursorLiElement.getAttribute("data-node-id");
            const liHTML = cursorLiElement.outerHTML;
            doOperation.push({action: "delete", id: liId});
            undoOperation.push({
                action: "insert",
                data: liHTML,
                id: liId,
                previousID: cursorLiElement.previousElementSibling?.getAttribute("data-node-id"),
                parentID: cursorLiElement.parentElement?.getAttribute("data-node-id")
            });
            cursorLiElement.remove();
        }
    }
    // Fix the ordered-list numbering after pasting https://github.com/siyuan-note/siyuan/issues/17890
    const orderLists = new Set<Element>();
    if (cursorLiElement) {
        // cursorLiElement may have already been cleaned up and removed, so reference it via parentList
        const cursorList = cursorLiElement.classList.contains("list") ? cursorLiElement : cursorLiElement.parentElement;
        if (cursorList?.getAttribute("data-subtype") === "o") {
            orderLists.add(cursorList);
        }
        // The list containing the last pasted item
        if (lastElement?.parentElement?.getAttribute("data-subtype") === "o") {
            orderLists.add(lastElement.parentElement);
        }
    }
    // A nested list produced by the paste may also be an ordered list
    doOperation.forEach(op => {
        if (op.action === "insert") {
            const tempEl = document.createElement("template");
            tempEl.innerHTML = op.data;
            tempEl.content.querySelectorAll("[data-type='NodeList'][data-subtype='o']").forEach(list => {
                const existing = protyle.wysiwyg.element.querySelector(`[data-node-id="${list.getAttribute("data-node-id")}"]`);
                if (existing) {
                    orderLists.add(existing);
                }
            });
        }
    });
    orderLists.forEach(orderList => {
        // Save the original state of existing list items, for undo
        const originalItems: {id: string, html: string}[] = [];
        orderList.querySelectorAll(":scope > .li").forEach(li => {
            const liId = li.getAttribute("data-node-id");
            if (!doOperation.find(o => o.id === liId && o.action === "insert")) {
                originalItems.push({id: liId, html: li.outerHTML});
            }
        });
        updateListOrder(orderList);
        // Update the data of affected list items in doOperation; add an update operation for existing items, for undo
        orderList.querySelectorAll(":scope > .li").forEach(li => {
            const liId = li.getAttribute("data-node-id");
            const op = doOperation.find(o => o.id === liId && o.action === "insert");
            if (op) {
                op.data = li.outerHTML;
            } else {
                const original = originalItems.find(item => item.id === liId);
                if (original && original.html !== li.outerHTML) {
                    doOperation.push({action: "update", id: liId, data: li.outerHTML});
                    undoOperation.push({action: "update", id: liId, data: original.html});
                }
            }
        });
    });
    transaction(protyle, doOperation, undoOperation);
};
