import {updateTransaction} from "../wysiwyg/transaction";
import {focusBlock, focusByRange, focusByWbr, getSelectionOffset, getSelectionPosition,} from "./selection";
import {hasClosestBlock, hasClosestByClassName, hasClosestByTag} from "./hasClosest";
import {matchHotKey} from "./hotKey";
import {isNotCtrl} from "./compatibility";
import {scrollCenter} from "../../util/highlightById";
import {insertEmptyBlock} from "../../block/util";
import {removeBlock} from "../wysiwyg/remove";
import {hasNextSibling, hasPreviousSibling} from "../wysiwyg/getBlock";
import * as dayjs from "dayjs";
import {Dialog} from "../../dialog";
import {isMobile} from "../../util/functions";

const scrollToView = (nodeElement: Element, rowElement: HTMLElement, protyle: IProtyle) => {
    if (nodeElement.getAttribute("custom-pinthead") === "true") {
        const tableElement = nodeElement.querySelector("table");
        if (tableElement.clientHeight + tableElement.scrollTop < rowElement.offsetTop + rowElement.clientHeight) {
            tableElement.scrollTop = rowElement.offsetTop - tableElement.clientHeight + rowElement.clientHeight + 1;
        } else if (tableElement.scrollTop > rowElement.offsetTop - rowElement.clientHeight) {
            tableElement.scrollTop = rowElement.offsetTop - rowElement.clientHeight + 1;
        }
    } else {
        scrollCenter(protyle, rowElement);
    }
};

export const getColIndex = (cellElement: HTMLElement) => {
    let previousElement = cellElement.previousElementSibling;
    let index = 0;
    while (previousElement) {
        index++;
        previousElement = previousElement.previousElementSibling;
    }
    return index;
};

// Set the caret to the previous cell
const goPreviousCell = (cellElement: HTMLElement, range: Range, isSelected = true) => {
    let previousElement = cellElement.previousElementSibling;
    if (!previousElement) {
        if (cellElement.parentElement.previousElementSibling) {
            previousElement = cellElement.parentElement.previousElementSibling.lastElementChild;
        } else if (cellElement.parentElement.parentElement.tagName === "TBODY" &&
            cellElement.parentElement.parentElement.previousElementSibling) {
            previousElement = cellElement.parentElement
                .parentElement.previousElementSibling.lastElementChild.lastElementChild;
        } else {
            previousElement = null;
        }
    }
    if (previousElement) {
        range.selectNodeContents(previousElement);
        if (!isSelected) {
            range.collapse(false);
        }
        focusByRange(range);
    }
    return previousElement;
};

export const setTableAlign = (protyle: IProtyle, cellElements: HTMLElement[], nodeElement: Element, type: string, range: Range) => {
    range.insertNode(document.createElement("wbr"));
    const html = nodeElement.outerHTML;

    const tableElement = nodeElement.querySelector("table");
    const columnCnt = tableElement.rows[0].cells.length;
    const rowCnt = tableElement.rows.length;
    const currentColumns: number[] = [];

    for (let i = 0; i < rowCnt; i++) {
        for (let j = 0; j < columnCnt; j++) {
            if (tableElement.rows[i].cells[j] === cellElements[currentColumns.length]) {
                currentColumns.push(j);
            }
        }
        if (currentColumns.length > 0) {
            break;
        }
    }
    for (let k = 0; k < rowCnt; k++) {
        currentColumns.forEach(item => {
            tableElement.rows[k].cells[item].setAttribute("align", type);
        });
    }
    updateTransaction(protyle, nodeElement, html);
    focusByWbr(tableElement, range);
};

export const insertRow = (protyle: IProtyle, range: Range, cellElement: HTMLElement, nodeElement: Element, count = 1) => {
    const wbrElement = document.createElement("wbr");
    range.insertNode(wbrElement);
    const html = nodeElement.outerHTML;
    wbrElement.remove();

    let rowHTML = "";
    for (let m = 0; m < cellElement.parentElement.childElementCount; m++) {
        rowHTML += `<td align="${cellElement.parentElement.children[m].getAttribute("align") || ""}"></td>`;
    }
    let newRowElement: HTMLTableRowElement;
    if (cellElement.tagName === "TH") {
        const tbodyElement = nodeElement.querySelector("tbody");
        if (tbodyElement) {
            tbodyElement.insertAdjacentHTML("afterbegin", `<tr>${rowHTML}</tr>`.repeat(count));
            newRowElement = tbodyElement.firstElementChild as HTMLTableRowElement;
        } else {
            cellElement.parentElement.parentElement.insertAdjacentHTML("afterend", `<tbody>${`<tr>${rowHTML}</tr>`.repeat(count)}</tbody>`);
            newRowElement = cellElement.parentElement.parentElement.nextElementSibling.firstElementChild as HTMLTableRowElement;
        }
    } else {
        cellElement.parentElement.insertAdjacentHTML("afterend", `<tr>${rowHTML}</tr>`.repeat(count));
        newRowElement = cellElement.parentElement.nextElementSibling as HTMLTableRowElement;
    }
    range.selectNodeContents(newRowElement.cells[getColIndex(cellElement)]);
    range.collapse(true);
    focusByRange(range);
    updateTransaction(protyle, nodeElement, html);
    scrollToView(nodeElement, newRowElement, protyle);
};

export const insertRowAbove = (protyle: IProtyle, range: Range, cellElement: HTMLElement, nodeElement: Element, count = 1) => {
    const wbrElement = document.createElement("wbr");
    range.insertNode(wbrElement);
    const html = nodeElement.outerHTML;
    wbrElement.remove();
    let rowHTML = "";
    let hasNone = false;

    for (let m = 0; m < cellElement.parentElement.childElementCount; m++) {
        const currentCellElement = cellElement.parentElement.children[m] as HTMLTableCellElement;
        const className = currentCellElement.className;
        if (className === "fn__none") {
            hasNone = true;
        }
        // No space is needed, otherwise inserting an image after the space would wrap once the column width is adjusted https://github.com/siyuan-note/siyuan/issues/7631
        const classAttr = className ? ` class="${className}"` : "";
        const tag = cellElement.tagName === "TH" ? "th" : "td";
        rowHTML += `<${tag}${classAttr} colspan="${currentCellElement.colSpan}" align="${currentCellElement.getAttribute("align") || ""}"></${tag}>`;
    }

    if (hasNone) {
        let previousTrElement = cellElement.parentElement.previousElementSibling;
        let rowCount = 1;
        while (previousTrElement) {
            rowCount++;
            Array.from(previousTrElement.children).forEach((cell: HTMLTableCellElement) => {
                if (cell.rowSpan >= rowCount && cell.rowSpan > 1) {
                    cell.rowSpan = cell.rowSpan + 1;
                }
            });
            previousTrElement = previousTrElement.previousElementSibling;
        }
    }
    let newRowElement: HTMLTableRowElement;
    if (cellElement.parentElement.parentElement.tagName === "THEAD" && !cellElement.parentElement.previousElementSibling) {
        cellElement.parentElement.parentElement.insertAdjacentHTML("beforebegin", `<thead><tr>${rowHTML}</tr></thead>`);
        newRowElement = nodeElement.querySelector("thead tr");
        cellElement.parentElement.parentElement.nextElementSibling.insertAdjacentHTML("afterbegin", cellElement.parentElement.parentElement.innerHTML.replace(/<th/g, "<td").replace(/<\/th>/g, "</td>"));
        if (count > 1) {
            cellElement.parentElement.parentElement.nextElementSibling.insertAdjacentHTML("afterbegin", `<tr>${rowHTML.replace(/<th/g, "<td").replace(/<\/th>/g, "</td>")}</tr>`.repeat(count - 1));
        }
        cellElement.parentElement.parentElement.remove();
    } else {
        cellElement.parentElement.insertAdjacentHTML("beforebegin", `<tr>${rowHTML}</tr>`.repeat(count));
        newRowElement = cellElement.parentElement.previousElementSibling as HTMLTableRowElement;
    }
    range.selectNodeContents(newRowElement.cells[getColIndex(cellElement)]);
    range.collapse(true);
    focusByRange(range);
    updateTransaction(protyle, nodeElement, html);
    scrollToView(nodeElement, newRowElement, protyle);
};

export const insertColumn = (protyle: IProtyle, nodeElement: Element, cellElement: HTMLElement, type: InsertPosition, range: Range, count = 1) => {
    const wbrElement = document.createElement("wbr");
    range.insertNode(wbrElement);
    const html = nodeElement.outerHTML;
    wbrElement.remove();
    const index = getColIndex(cellElement);
    const tableElement = nodeElement.querySelector("table");
    for (let i = 0; i < tableElement.rows.length; i++) {
        const colCellElement = tableElement.rows[i].cells[index];
        const tag = colCellElement.tagName.toLowerCase();
        let html = "";
        if (colCellElement === cellElement) {
            html = `<${tag}><wbr></${tag}>` + `<${tag}></${tag}>`.repeat(count - 1);
        } else {
            html = `<${tag}></${tag}>`.repeat(count);
        }
        colCellElement.insertAdjacentHTML(type, html);
    }
    // Horizontal scrollbar positioning
    if (type === "afterend" && cellElement.offsetLeft + cellElement.clientWidth + 60 >
        nodeElement.firstElementChild.scrollLeft + nodeElement.firstElementChild.clientWidth) {
        nodeElement.firstElementChild.scrollLeft = cellElement.offsetLeft + cellElement.clientWidth + 60 - nodeElement.firstElementChild.clientWidth;
    } else if (type === "beforebegin" && cellElement.offsetLeft - 60 * count < nodeElement.firstElementChild.scrollLeft) {
        nodeElement.firstElementChild.scrollLeft = cellElement.offsetLeft - 60 * count;
    }
    tableElement.querySelectorAll("col")[index].insertAdjacentHTML(type, "<col style='min-width: 60px;'>".repeat(count));
    focusByWbr(nodeElement, range);
    updateTransaction(protyle, nodeElement, html);
};

export const deleteRow = (protyle: IProtyle, range: Range, cellElement: HTMLElement, nodeElement: Element) => {
    if (cellElement.parentElement.parentElement.tagName !== "THEAD") {
        const wbrElement = document.createElement("wbr");
        range.insertNode(wbrElement);
        const html = nodeElement.outerHTML;
        wbrElement.remove();
        const index = getColIndex(cellElement);
        const tbodyElement = cellElement.parentElement.parentElement;
        let previousTrElement = tbodyElement.previousElementSibling.lastElementChild as HTMLTableRowElement;
        if (cellElement.parentElement.previousElementSibling) {
            previousTrElement = cellElement.parentElement.previousElementSibling as HTMLTableRowElement;
        }

        if (tbodyElement.childElementCount === 1) {
            tbodyElement.remove();
        } else {
            cellElement.parentElement.remove();
        }
        range.selectNodeContents(previousTrElement.cells[index]);
        range.collapse(true);
        focusByRange(range);
        scrollToView(nodeElement, previousTrElement, protyle);
        updateTransaction(protyle, nodeElement, html);
    }
};

export const deleteColumn = (protyle: IProtyle, range: Range, nodeElement: Element, cellElement: HTMLElement) => {
    const wbrElement = document.createElement("wbr");
    range.insertNode(wbrElement);
    const html = nodeElement.outerHTML;
    wbrElement.remove();
    const index = getColIndex(cellElement);
    const sideCellElement = (cellElement.previousElementSibling || cellElement.nextElementSibling) as HTMLElement;
    if (sideCellElement) {
        range.selectNodeContents(sideCellElement);
        range.collapse(true);
        // Horizontal scrollbar positioning
        if (sideCellElement.offsetLeft + sideCellElement.clientWidth > nodeElement.firstElementChild.scrollLeft + nodeElement.firstElementChild.clientWidth) {
            nodeElement.firstElementChild.scrollLeft = sideCellElement.offsetLeft + sideCellElement.clientWidth - nodeElement.firstElementChild.clientWidth;
        }
    } else {
        nodeElement.classList.add("protyle-wysiwyg--select");
        removeBlock(protyle, nodeElement, range, "remove");
        return;
    }
    const tableElement = nodeElement.querySelector("table");
    for (let i = 0; i < tableElement.rows.length; i++) {
        const cells = tableElement.rows[i].cells;
        if (cells.length === 1) {
            tableElement.remove();
            break;
        }
        cells[index].remove();
    }
    nodeElement.querySelectorAll("col")[index]?.remove();
    updateTransaction(protyle, nodeElement, html);
    focusByRange(range);
};

export const moveRowToUp = (protyle: IProtyle, range: Range, cellElement: HTMLElement, nodeElement: Element) => {
    const rowElement = cellElement.parentElement;
    if (rowElement.parentElement.tagName === "THEAD") {
        return;
    }
    range.insertNode(document.createElement("wbr"));
    const html = nodeElement.outerHTML;
    if (rowElement.previousElementSibling) {
        rowElement.after(rowElement.previousElementSibling);
    } else {
        const headElement = rowElement.parentElement.previousElementSibling.firstElementChild;
        headElement.querySelectorAll("th").forEach(item => {
            const tdElement = document.createElement("td");
            tdElement.innerHTML = item.innerHTML;
            item.parentNode.replaceChild(tdElement, item);
        });
        rowElement.querySelectorAll("td").forEach(item => {
            const thElement = document.createElement("th");
            thElement.innerHTML = item.innerHTML;
            item.parentNode.replaceChild(thElement, item);
        });
        rowElement.after(headElement);
        rowElement.parentElement.previousElementSibling.append(rowElement);
    }
    updateTransaction(protyle, nodeElement, html);
    focusByWbr(nodeElement, range);
    scrollCenter(protyle, rowElement);
};

export const moveRowToDown = (protyle: IProtyle, range: Range, cellElement: HTMLElement, nodeElement: Element) => {
    const rowElement = cellElement.parentElement;
    if ((rowElement.parentElement.tagName === "TBODY" && !rowElement.nextElementSibling) ||
        (rowElement.parentElement.tagName === "THEAD" && !rowElement.parentElement.nextElementSibling)) {
        return;
    }
    range.insertNode(document.createElement("wbr"));
    const html = nodeElement.outerHTML;
    if (rowElement.nextElementSibling) {
        rowElement.before(rowElement.nextElementSibling);
    } else {
        const firstRowElement = rowElement.parentElement.nextElementSibling.firstElementChild;
        firstRowElement.querySelectorAll("td").forEach(item => {
            const thElement = document.createElement("th");
            thElement.innerHTML = item.innerHTML;
            item.parentNode.replaceChild(thElement, item);
        });
        rowElement.querySelectorAll("th").forEach(item => {
            const tdElement = document.createElement("td");
            tdElement.innerHTML = item.innerHTML;
            item.parentNode.replaceChild(tdElement, item);
        });
        rowElement.after(firstRowElement);
        rowElement.parentElement.nextElementSibling.insertAdjacentElement("afterbegin", rowElement);
    }
    updateTransaction(protyle, nodeElement, html);
    focusByWbr(nodeElement, range);
    scrollCenter(protyle, rowElement);
};

export const moveColumnToLeft = (protyle: IProtyle, range: Range, cellElement: HTMLElement, nodeElement: Element) => {
    if (!cellElement.previousElementSibling) {
        return;
    }
    range.insertNode(document.createElement("wbr"));
    const html = nodeElement.outerHTML;
    let cellIndex = 0;
    Array.from(cellElement.parentElement.children).find((item, index) => {
        if (cellElement === item) {
            cellIndex = index;
            return true;
        }
    });

    nodeElement.querySelectorAll("tr").forEach((trElement) => {
        trElement.cells[cellIndex].after(trElement.cells[cellIndex - 1]);
    });
    // Horizontal scrollbar positioning
    if (cellElement.offsetLeft < nodeElement.firstElementChild.scrollLeft) {
        nodeElement.firstElementChild.scrollLeft = cellElement.offsetLeft;
    }
    const colElements = nodeElement.querySelectorAll("col");
    colElements[cellIndex].after(colElements[cellIndex - 1]);
    updateTransaction(protyle, nodeElement, html);
    focusByWbr(nodeElement, range);
};

export const moveColumnToRight = (protyle: IProtyle, range: Range, cellElement: HTMLElement, nodeElement: Element) => {
    if (!cellElement.nextElementSibling) {
        return;
    }
    range.insertNode(document.createElement("wbr"));
    const html = nodeElement.outerHTML;
    let cellIndex = 0;
    Array.from(cellElement.parentElement.children).find((item, index) => {
        if (cellElement === item) {
            cellIndex = index;
            return true;
        }
    });
    nodeElement.querySelectorAll("tr").forEach((trElement) => {
        trElement.cells[cellIndex].before(trElement.cells[cellIndex + 1]);
    });
    // Horizontal scrollbar positioning
    if (cellElement.offsetLeft + cellElement.clientWidth > nodeElement.firstElementChild.scrollLeft + nodeElement.firstElementChild.clientWidth) {
        nodeElement.firstElementChild.scrollLeft = cellElement.offsetLeft + cellElement.clientWidth - nodeElement.firstElementChild.clientWidth;
    }
    const colElements = nodeElement.querySelectorAll("col");
    colElements[cellIndex].before(colElements[cellIndex + 1]);
    updateTransaction(protyle, nodeElement, html);
    focusByWbr(nodeElement, range);
};

export const fixTable = (protyle: IProtyle, event: KeyboardEvent, range: Range) => {
    const cellElement = (hasClosestByTag(range.startContainer, "TD") || hasClosestByTag(range.startContainer, "TH")) as HTMLTableCellElement;
    const nodeElement = hasClosestBlock(range.startContainer) as HTMLTableElement;
    if (!cellElement || !nodeElement) {
        return false;
    }
    // The caret is inside a table, but pressing delete does nothing once another block has been selected
    const selectedElement = protyle.wysiwyg.element.querySelector(".protyle-wysiwyg--select");
    if (selectedElement && !selectedElement.contains(cellElement)) {
        return false;
    }
    if (event.key === "Backspace" && range.toString() === "") {
        const previousElement = hasPreviousSibling(range.startContainer) as Element;
        if (range.startOffset === 1 && previousElement.nodeType === 1 && previousElement.tagName === "BR" &&
            range.startContainer.textContent.length === 1 && !hasNextSibling(range.startContainer)) {
            previousElement.insertAdjacentHTML("beforebegin", "<br>");
            return false;
        }
    }

    // shift+enter soft line break
    if (event.key === "Enter" && event.shiftKey && isNotCtrl(event) && !event.altKey) {
        const wbrElement = document.createElement("wbr");
        range.insertNode(wbrElement);
        const oldHTML = nodeElement.outerHTML;
        wbrElement.remove();
        if (cellElement && !cellElement.innerHTML.endsWith("<br>")) {
            cellElement.insertAdjacentHTML("beforeend", "<br>");
        }
        range.extractContents();
        const types = protyle.toolbar.getCurrentType(range);
        if (types.includes("code") && range.startContainer.nodeType !== 3) {
            // https://github.com/siyuan-note/siyuan/issues/4169
            const brElement = document.createElement("br");
            (range.startContainer as HTMLElement).after(brElement);
            range.setStartAfter(brElement);
        } else {
            range.insertNode(document.createElement("br"));
        }
        range.collapse(false);
        scrollCenter(protyle);
        updateTransaction(protyle, nodeElement, oldHTML);
        event.preventDefault();
        return true;
    }

    if (!nodeElement.classList.contains("protyle-wysiwyg--select") && !hasClosestByClassName(nodeElement, "protyle-wysiwyg--select")) {
        // Enter moves the caret to the same column in the next row
        if (isNotCtrl(event) && !event.shiftKey && !event.altKey && event.key === "Enter") {
            event.preventDefault();
            const trElement = cellElement.parentElement as HTMLTableRowElement;
            if ((!trElement.nextElementSibling && trElement.parentElement.tagName === "TBODY") ||
                (trElement.parentElement.tagName === "THEAD" && !trElement.parentElement.nextElementSibling)) {
                insertEmptyBlock(protyle, "afterend", nodeElement.getAttribute("data-node-id"));
                return true;
            }
            let nextElement = trElement.nextElementSibling as HTMLTableRowElement;
            if (!nextElement) {
                nextElement = trElement.parentElement.nextElementSibling.firstChild as HTMLTableRowElement;
            }
            if (!nextElement) {
                return true;
            }
            range.selectNodeContents(nextElement.cells[getColIndex(cellElement)]);
            range.collapse(true);
            scrollCenter(protyle);
            return true;
        }
        // When there's no content after the table, pressing ArrowRight needs to create a new empty block
        if (event.key === "ArrowRight" && range.toString() === "" &&
            !nodeElement.nextElementSibling &&
            cellElement === nodeElement.querySelector("table").lastElementChild.lastElementChild.lastElementChild &&
            getSelectionOffset(cellElement, protyle.wysiwyg.element, range).start === cellElement.innerText.length) {
            event.preventDefault();
            insertEmptyBlock(protyle, "afterend", nodeElement.getAttribute("data-node-id"));
            return true;
        }
        // Tab: move the caret to the next cell
        if (event.key === "Tab" && isNotCtrl(event)) {
            if (event.shiftKey) {
                // shift + tab moves the caret to the previous cell
                goPreviousCell(cellElement, range);
                event.preventDefault();
                return true;
            }

            let nextElement = cellElement.nextElementSibling;
            if (!nextElement) {
                if (cellElement.parentElement.nextElementSibling) {
                    nextElement = cellElement.parentElement.nextElementSibling.firstElementChild;
                } else if (cellElement.parentElement.parentElement.tagName === "THEAD" &&
                    cellElement.parentElement.parentElement.nextElementSibling) {
                    nextElement =
                        cellElement.parentElement.parentElement.nextElementSibling.firstElementChild.firstElementChild;
                } else {
                    nextElement = null;
                }
            }
            if (nextElement) {
                range.selectNodeContents(nextElement);
            } else {
                insertRow(protyle, range, cellElement.parentElement.firstElementChild as HTMLTableCellElement, nodeElement);
            }
            event.preventDefault();
            return true;
        }

        if (event.key === "ArrowUp" && isNotCtrl(event) && !event.shiftKey && !event.altKey) {
            if (cellElement.firstChild) {
                let firstChild = cellElement.firstChild;
                while (firstChild) {
                    if (firstChild.textContent === "" && firstChild.nodeType === 3) {
                        if (!firstChild.nextSibling) {
                            break;
                        }
                        firstChild = firstChild.nextSibling;
                    } else {
                        break;
                    }
                }
                const rangeTemp = document.createRange();
                rangeTemp.selectNodeContents(firstChild);
                rangeTemp.collapse(true);
                const rangeRects = range.getClientRects().length === 0 ? getSelectionPosition(cellElement, range) : range.getClientRects()[0];
                const rangeTempRects = rangeTemp.getClientRects().length === 0 ? getSelectionPosition(cellElement, rangeTemp) : rangeTemp.getClientRects()[0];
                if (rangeTempRects.top < rangeRects.top) {
                    return false;
                }
            }
            const trElement = cellElement.parentElement as HTMLTableRowElement;
            let previousElement = trElement.previousElementSibling as HTMLTableRowElement;
            if (!previousElement) {
                previousElement = trElement.parentElement.previousElementSibling.lastElementChild as HTMLTableRowElement;
            }
            if (!previousElement || previousElement?.tagName === "COL") {
                return false;
            }
            const currentColIndex = getColIndex(cellElement);
            let newCellElement = previousElement.cells[currentColIndex];
            while (previousElement) {
                let i = 0;
                while (newCellElement && newCellElement.classList.contains("fn__none")) {
                    i++;
                    newCellElement = newCellElement.previousElementSibling as HTMLTableCellElement;
                }
                if (newCellElement.colSpan < 2 && i !== 0) {
                    previousElement = previousElement.previousElementSibling as HTMLTableRowElement;
                    newCellElement = previousElement.cells[currentColIndex];
                } else if (newCellElement.colSpan > i) {
                    break;
                }
            }

            range.selectNodeContents(newCellElement);
            range.collapse(false);
            scrollCenter(protyle);
            event.preventDefault();
            return true;
        }

        if (event.key === "ArrowDown" && isNotCtrl(event) && !event.shiftKey && !event.altKey) {
            if (cellElement.lastChild) {
                let lastChild = cellElement.lastChild;
                while (lastChild) {
                    if (lastChild.textContent === "" && lastChild.nodeType === 3) {
                        if (!lastChild.previousSibling) {
                            break;
                        }
                        lastChild = lastChild.previousSibling;
                    } else {
                        break;
                    }
                }
                const rangeTemp = document.createRange();
                rangeTemp.selectNodeContents(lastChild);
                rangeTemp.collapse(false);
                if (getSelectionPosition(cellElement, rangeTemp).top > getSelectionPosition(cellElement, range).top) {
                    return false;
                }
            }
            const trElement = cellElement.parentElement as HTMLTableRowElement;
            if ((!trElement.nextElementSibling && trElement.parentElement.tagName === "TBODY") ||
                (trElement.parentElement.tagName === "THEAD" && !trElement.parentElement.nextElementSibling)) {
                return false;
            }
            let nextElement = trElement.nextElementSibling as HTMLTableRowElement;
            if (!nextElement) {
                nextElement = trElement.parentElement.nextElementSibling.firstChild as HTMLTableRowElement;
            }
            if (!nextElement) {
                return false;
            }
            let rowSpan = cellElement.rowSpan;
            while (rowSpan > 1) {
                rowSpan--;
                nextElement = nextElement.nextElementSibling as HTMLTableRowElement;
            }
            let nextCellElement = nextElement.cells[getColIndex(cellElement)];
            while (nextCellElement.classList.contains("fn__none") && nextCellElement.nextElementSibling) {
                nextCellElement = nextCellElement.previousElementSibling as HTMLTableCellElement;
            }
            range.selectNodeContents(nextCellElement);
            range.collapse(true);
            scrollCenter(protyle);
            event.preventDefault();
            return true;
        }

        // Backspace: move the caret to the previous cell
        if (isNotCtrl(event) && !event.shiftKey && !event.altKey && event.key === "Backspace"
            && getSelectionOffset(cellElement, protyle.wysiwyg.element, range).start === 0 && range.toString() === "" &&
            // An empty line break can't be deleted https://github.com/siyuan-note/siyuan/issues/2732
            (range.startOffset === 0 || (range.startOffset === 1 && cellElement.querySelectorAll("br").length === 1))) {
            const previousCellElement = goPreviousCell(cellElement, range, false);
            if (!previousCellElement && nodeElement.previousElementSibling) {
                focusBlock(nodeElement.previousElementSibling, undefined, false);
            }
            scrollCenter(protyle);
            event.preventDefault();
            return true;
        }

        // Align left
        if (matchHotKey(window.siyuan.config.keymap.editor.general.alignLeft.custom, event)) {
            setTableAlign(protyle, [cellElement], nodeElement, "left", range);
            event.preventDefault();
            return true;
        }
        // Align center
        if (matchHotKey(window.siyuan.config.keymap.editor.general.alignCenter.custom, event)) {
            setTableAlign(protyle, [cellElement], nodeElement, "center", range);
            event.preventDefault();
            return true;
        }
        // Align right
        if (matchHotKey(window.siyuan.config.keymap.editor.general.alignRight.custom, event)) {
            setTableAlign(protyle, [cellElement], nodeElement, "right", range);
            event.preventDefault();
            return true;
        }
    }

    const tableElement = nodeElement.querySelector("table");
    const hasNone = cellElement.parentElement.querySelector(".fn__none");
    let hasColSpan = false;
    let hasRowSpan = false;
    Array.from(cellElement.parentElement.children).forEach((item: HTMLTableCellElement) => {
        if (item.colSpan > 1) {
            hasColSpan = true;
        }
        if (item.rowSpan > 1) {
            hasRowSpan = true;
        }
    });
    let previousHasNone: false | Element = false;
    let previousHasColSpan = false;
    let previousHasRowSpan = false;
    let previousRowElement = cellElement.parentElement.previousElementSibling;
    if (!previousRowElement && cellElement.parentElement.parentElement.tagName === "TBODY") {
        previousRowElement = tableElement.querySelector("thead").lastElementChild;
    }
    if (previousRowElement) {
        previousHasNone = previousRowElement.querySelector(".fn__none");
        Array.from(previousRowElement.children).forEach((item: HTMLTableCellElement) => {
            if (item.colSpan > 1) {
                previousHasColSpan = true;
            }
            if (item.rowSpan > 1) {
                previousHasRowSpan = true;
            }
        });
    }
    let nextHasNone: false | Element = false;
    let nextHasColSpan = false;
    let nextHasRowSpan = false;
    let nextRowElement = cellElement.parentElement.nextElementSibling;
    if (!nextRowElement && cellElement.parentElement.parentElement.tagName === "THEAD") {
        nextRowElement = tableElement.querySelector("tbody")?.firstElementChild;
    }
    if (nextRowElement) {
        nextHasNone = nextRowElement.querySelector(".fn__none");
        Array.from(nextRowElement.children).forEach((item: HTMLTableCellElement) => {
            if (item.colSpan > 1) {
                nextHasColSpan = true;
            }
            if (item.rowSpan > 1) {
                nextHasRowSpan = true;
            }
        });
    }
    const colIndex = getColIndex(cellElement);
    let colIsPure = true;
    Array.from(tableElement.rows).find(item => {
        const cellElement = item.cells[colIndex];
        if (cellElement.classList.contains("fn__none") || cellElement.colSpan > 1 || cellElement.rowSpan > 1) {
            colIsPure = false;
            return true;
        }
    });
    let nextColIsPure = true;
    Array.from(tableElement.rows).find(item => {
        const cellElement = item.cells[colIndex + 1];
        if (cellElement && (cellElement.classList.contains("fn__none") || cellElement.colSpan > 1 || cellElement.rowSpan > 1)) {
            nextColIsPure = false;
            return true;
        }
    });
    let previousColIsPure = true;
    Array.from(tableElement.rows).find(item => {
        const cellElement = item.cells[colIndex - 1];
        if (cellElement && (cellElement.classList.contains("fn__none") || cellElement.colSpan > 1 || cellElement.rowSpan > 1)) {
            previousColIsPure = false;
            return true;
        }
    });
    if (matchHotKey(window.siyuan.config.keymap.editor.table.moveToUp.custom, event)) {
        if ((!hasNone || (hasNone && !hasRowSpan && hasColSpan)) &&
            (!previousHasNone || (previousHasNone && !previousHasRowSpan && previousHasColSpan))) {
            moveRowToUp(protyle, range, cellElement, nodeElement);
        }
        event.preventDefault();
        return true;
    }

    if (matchHotKey(window.siyuan.config.keymap.editor.table.moveToDown.custom, event)) {
        if ((!hasNone || (hasNone && !hasRowSpan && hasColSpan)) &&
            (!nextHasNone || (nextHasNone && !nextHasRowSpan && nextHasColSpan))) {
            moveRowToDown(protyle, range, cellElement, nodeElement);
        }
        event.preventDefault();
        return true;
    }

    if (matchHotKey(window.siyuan.config.keymap.editor.table.moveToLeft.custom, event)) {
        if (colIsPure && previousColIsPure) {
            moveColumnToLeft(protyle, range, cellElement, nodeElement);
        }
        event.preventDefault();
        return true;
    }

    if (matchHotKey(window.siyuan.config.keymap.editor.table.moveToRight.custom, event)) {
        if (colIsPure && nextColIsPure) {
            moveColumnToRight(protyle, range, cellElement, nodeElement);
        }
        event.preventDefault();
        return true;
    }

    // Insert a new row above
    if (matchHotKey(window.siyuan.config.keymap.editor.table.insertRowAbove.custom, event)) {
        insertRowAbove(protyle, range, cellElement, nodeElement);
        event.preventDefault();
        event.stopPropagation();
        return true;
    }

    // Insert a new row below https://github.com/Vanessa219/vditor/issues/46
    if (matchHotKey(window.siyuan.config.keymap.editor.table.insertRowBelow.custom, event)) {
        if (!nextHasNone || (nextHasNone && !nextHasRowSpan && nextHasColSpan)) {
            insertRow(protyle, range, cellElement, nodeElement);
        }
        event.preventDefault();
        return true;
    }

    // Insert a new column to the left
    if (matchHotKey(window.siyuan.config.keymap.editor.table.insertColumnLeft.custom, event)) {
        if (colIsPure || previousColIsPure) {
            insertColumn(protyle, nodeElement, cellElement, "beforebegin", range);
        }
        event.preventDefault();
        return true;
    }

    // Insert a new column to the right
    if (matchHotKey(window.siyuan.config.keymap.editor.table.insertColumnRight.custom, event)) {
        if (colIsPure || nextColIsPure) {
            insertColumn(protyle, nodeElement, cellElement, "afterend", range);
        }
        event.preventDefault();
        return true;
    }

    // Delete the current row
    if (matchHotKey(window.siyuan.config.keymap.editor.table["delete-row"].custom, event)) {
        if ((!hasNone && !hasRowSpan) || //https://github.com/siyuan-note/siyuan/issues/5045
            (hasNone && !hasRowSpan && hasColSpan)) {
            deleteRow(protyle, range, cellElement, nodeElement);
        }
        event.preventDefault();
        event.stopPropagation();
        return true;
    }

    // Delete the current column
    if (matchHotKey(window.siyuan.config.keymap.editor.table["delete-column"].custom, event)) {
        if (colIsPure) {
            deleteColumn(protyle, range, nodeElement, cellElement);
        }
        event.preventDefault();
        return true;
    }
};

export const isIncludeCell = (options: {
    tableSelectElement: HTMLElement,
    scrollLeft: number,
    scrollTop: number,
    item: HTMLTableCellElement,
}) => {
    if (options.item.offsetLeft + 6 > options.tableSelectElement.offsetLeft + options.scrollLeft &&
        options.item.offsetLeft + options.item.clientWidth - 6 < options.tableSelectElement.offsetLeft + options.scrollLeft + options.tableSelectElement.clientWidth &&
        options.item.offsetTop + 6 > options.tableSelectElement.offsetTop + options.scrollTop &&
        options.item.offsetTop + options.item.clientHeight - 6 < options.tableSelectElement.offsetTop + options.scrollTop + options.tableSelectElement.clientHeight) {
        return true;
    }
    return false;
};

export const clearTableCell = (protyle: IProtyle, tableBlockElement: HTMLElement) => {
    if (!tableBlockElement) {
        return;
    }
    const tableSelectElement = tableBlockElement.querySelector(".table__select") as HTMLElement;
    const selectCellElements: HTMLTableCellElement[] = [];
    const scrollLeft = tableBlockElement.firstElementChild.scrollLeft;
    const scrollTop = tableBlockElement.querySelector("table").scrollTop;
    tableBlockElement.querySelectorAll("th, td").forEach((item: HTMLTableCellElement) => {
        if (!item.classList.contains("fn__none") && isIncludeCell({
            tableSelectElement,
            scrollLeft,
            scrollTop,
            item,
        })) {
            selectCellElements.push(item);
        }
    });
    tableSelectElement.removeAttribute("style");
    if (getSelection().rangeCount > 0) {
        const range = getSelection().getRangeAt(0);
        if (tableBlockElement.contains(range.startContainer)) {
            range.insertNode(document.createElement("wbr"));
        }
    }
    const oldHTML = tableBlockElement.outerHTML;
    tableBlockElement.querySelector("wbr")?.remove();
    tableBlockElement.setAttribute("updated", dayjs().format("YYYYMMDDHHmmss"));
    selectCellElements.forEach(item => {
        item.innerHTML = "";
    });
    updateTransaction(protyle, tableBlockElement, oldHTML);
};

export const updateTableTitle = (protyle: IProtyle, nodeElement: Element) => {
    const captionElement = nodeElement.querySelector("caption");
    window.siyuan.menus.menu.remove();
    const dialog = new Dialog({
        title: window.siyuan.languages.table,
        width: isMobile() ? "92vw" : "520px",
        content: `<div class="b3-dialog__content">
    <label>
        <div>${window.siyuan.languages.title}</div>
        <div class="fn__hr"></div>
        <input class="b3-text-field fn__block">
    </label>
    <div class="fn__hr--b"></div>
    <label>
        <div>${window.siyuan.languages.position}</div>
        <div class="fn__hr"></div>
        <select class="b3-select fn__block">
            <option value="top">${window.siyuan.languages.up}</option>
            <option value="bottom" ${captionElement?.style.captionSide === "bottom" ? "selected" : ""}>${window.siyuan.languages.down}</option>
        </select>
    </label>
</div>
<div class="b3-dialog__action">
    <button class="b3-button b3-button--cancel">${window.siyuan.languages.cancel}</button><div class="fn__space"></div>
    <button class="b3-button b3-button--text">${window.siyuan.languages.confirm}</button>
</div>
<div>`,
    });
    const html = nodeElement.outerHTML;
    const inputElement = dialog.element.querySelector(".b3-text-field") as HTMLInputElement;
    const btnsElement = dialog.element.querySelectorAll(".b3-button");
    dialog.bindInput(inputElement, () => {
        (btnsElement[1] as HTMLButtonElement).click();
    });
    btnsElement[0].addEventListener("click", () => {
        dialog.destroy();
    });
    btnsElement[1].addEventListener("click", () => {
        const title = inputElement.value.trim();
        const location = (dialog.element.querySelector("select") as HTMLSelectElement).value;
        if (title) {
            const html = `<caption contenteditable="false" ${location === "bottom" ? 'style="caption-side: bottom;"' : ""}>${Lute.EscapeHTMLStr(title)}</caption>`;
            if (captionElement) {
                captionElement.outerHTML = html;
            } else {
                nodeElement.querySelector("table").insertAdjacentHTML("afterbegin", html);
            }
            nodeElement.setAttribute("caption", Lute.EscapeHTMLStr(html));
        } else {
            if (captionElement) {
                captionElement.remove();
            }
            nodeElement.removeAttribute("caption");
        }
        updateTransaction(protyle, nodeElement, html);
        dialog.destroy();
    });
    inputElement.value = captionElement?.textContent || "";
    inputElement.focus();
    inputElement.select();
};

interface ITableCellInfo {
    cell: HTMLTableCellElement;
    row: number;
    col: number;
    rowspan: number;
    colspan: number;
}

interface ITableGrid {
    cellInfos: ITableCellInfo[];
    sectionOfRow: string[];
    rowCount: number;
}

export interface ITableRangeCell {
    cell: HTMLTableCellElement;
    row: number;
    col: number;
}

const buildTableGrid = (tableElement: HTMLElement): ITableGrid => {
    const cellInfos: ITableCellInfo[] = [];
    const sectionOfRow: string[] = [];
    const grid: (HTMLTableCellElement | null)[][] = [];
    const getCS = (cell: HTMLTableCellElement, attr: string) => {
        const v = cell.getAttribute(attr);
        if (!v) {
            return 1;
        }
        const n = parseInt(v, 10);
        return isNaN(n) || n < 1 ? 1 : n;
    };
    const ensureRow = (r: number) => {
        while (grid.length <= r) {
            grid.push([]);
            sectionOfRow.push("");
        }
    };
    const trElements = Array.from(tableElement.querySelectorAll("tr"));
    trElements.forEach((tr, rowIdx) => {
        ensureRow(rowIdx);
        // Determine which section this tr belongs to
        const section = (tr.parentElement && (tr.parentElement.tagName === "THEAD")) ? "thead" : "tbody";
        sectionOfRow[rowIdx] = section;
        let colIdx = 0;
        tr.querySelectorAll("th, td").forEach((cell: HTMLTableCellElement) => {
            if (cell.classList.contains("fn__none")) {
                return; // Skip merged-cell placeholders
            }
            const rowspan = getCS(cell, "rowspan");
            const colspan = getCS(cell, "colspan");
            // Find the first free column in the current row
            while (grid[rowIdx][colIdx]) {
                colIdx++;
            }
            cellInfos.push({cell, row: rowIdx, col: colIdx, rowspan, colspan});
            // Occupy the grid
            for (let dr = 0; dr < rowspan; dr++) {
                ensureRow(rowIdx + dr);
                for (let dc = 0; dc < colspan; dc++) {
                    grid[rowIdx + dr][colIdx + dc] = cell;
                }
            }
            colIdx += colspan;
        });
    });

    return {cellInfos, sectionOfRow, rowCount: trElements.length};
};

const getTableRangeBounds = (cellInfos: ITableCellInfo[], rowCount: number, startCell: HTMLElement, endCell: HTMLElement) => {
    const startInfo = cellInfos.find(info => info.cell === startCell);
    const endInfo = cellInfos.find(info => info.cell === endCell);
    if (!startInfo || !endInfo) {
        return undefined;
    }
    return {
        rowStart: Math.min(startInfo.row, endInfo.row),
        // Legacy data may have a rowspan that extends past the table's last row; copying must not generate a
        // virtual trailing row containing only fn__none for it.
        rowEnd: Math.min(rowCount - 1,
            Math.max(startInfo.row + startInfo.rowspan - 1, endInfo.row + endInfo.rowspan - 1)),
        colStart: Math.min(startInfo.col, endInfo.col),
        colEnd: Math.max(startInfo.col + startInfo.colspan - 1, endInfo.col + endInfo.colspan - 1),
    };
};

// Returns the actual editable cells within the selection and their relative grid coordinates; merged-cell
// placeholders are excluded from the result.
export const getTableRangeCells = (tableElement: HTMLElement, startCell?: HTMLElement, endCell?: HTMLElement) => {
    const {cellInfos, rowCount} = buildTableGrid(tableElement);
    if (!startCell || !endCell) {
        return cellInfos.map(info => ({cell: info.cell, row: info.row, col: info.col}));
    }
    const bounds = getTableRangeBounds(cellInfos, rowCount, startCell, endCell);
    if (!bounds) {
        return [];
    }
    const ret: ITableRangeCell[] = [];
    cellInfos.forEach(info => {
        const row = Math.max(info.row, bounds.rowStart);
        const rowEnd = Math.min(info.row + info.rowspan - 1, bounds.rowEnd);
        const col = Math.max(info.col, bounds.colStart);
        const colEnd = Math.min(info.col + info.colspan - 1, bounds.colEnd);
        if (row <= rowEnd && col <= colEnd) {
            ret.push({cell: info.cell, row: row - bounds.rowStart, col: col - bounds.colStart});
        }
    });
    return ret;
};

// getTableRangeHTML rebuilds a valid <table> HTML from the rectangular region spanning the start cell to the end cell.
// Used for copying/cutting a text selection that spans multiple table cells: the original
// range.cloneContents()/extractContents() would produce broken fragments.
// Algorithm: build a 2D grid mapping of the original table, determine the selection's grid range, enumerate the
// physical cells within it, and recalculate colspan/rowspan for each cell based on its actual span within the
// new table (the selection), avoiding dimension misalignment.
export const getTableRangeHTML = (tableElement: HTMLElement, startCell: HTMLElement, endCell: HTMLElement) => {
    // 1. Build the 2D grid mapping, recording each physical cell's grid coordinates, span, and the row it
    // belongs to (used to preserve the thead/tbody split)
    // grid[r][c] = cell (each cell occupies rowspan x colspan grid positions)
    const {cellInfos, sectionOfRow, rowCount} = buildTableGrid(tableElement);

    // 2. Determine the grid coordinates of startCell/endCell
    const bounds = getTableRangeBounds(cellInfos, rowCount, startCell, endCell);
    if (!bounds) {
        return "";
    }

    // 3. Compute the selection's grid range (including the merge spans of startCell/endCell themselves)
    const selRowStart = bounds.rowStart;
    const selRowEnd = bounds.rowEnd;
    const selColStart = bounds.colStart;
    const selColEnd = bounds.colEnd;

    // 4. Enumerate cells that intersect the selection, computing their row, column, and span in the new table
    type OutCell = {
        newCell: HTMLTableCellElement;
        newRow: number;
        newCol: number;
        newRowspan: number;
        newColspan: number
    };
    const outCells: OutCell[] = [];
    cellInfos.forEach(info => {
        // Check whether this cell's grid range intersects the selection
        const interRowStart = Math.max(info.row, selRowStart);
        const interRowEnd = Math.min(info.row + info.rowspan - 1, selRowEnd);
        const interColStart = Math.max(info.col, selColStart);
        const interColEnd = Math.min(info.col + info.colspan - 1, selColEnd);
        if (interRowStart > interRowEnd || interColStart > interColEnd) {
            return; // No intersection
        }
        // Recompute the span within the new table (= the span of the intersecting part)
        const newRow = interRowStart - selRowStart;
        const newCol = interColStart - selColStart;
        const newRowspan = interRowEnd - interRowStart + 1;
        const newColspan = interColEnd - interColStart + 1;
        const newCell = info.cell.cloneNode(true) as HTMLTableCellElement;
        // Remove the placeholder class (to avoid it being mistaken for fn__none)
        newCell.classList.remove("fn__none");
        if (newRowspan > 1) {
            newCell.setAttribute("rowspan", String(newRowspan));
        } else {
            newCell.removeAttribute("rowspan");
        }
        if (newColspan > 1) {
            newCell.setAttribute("colspan", String(newColspan));
        } else {
            newCell.removeAttribute("colspan");
        }
        outCells.push({newCell, newRow, newCol, newRowspan, newColspan});
    });

    // 5. Output by new row/column number. An output grid is needed to correctly handle rowspan placeholders:
    // when a cell's newRowspan > 1 spans multiple rows, the corresponding column in the following rows must
    // have a class="fn__none" placeholder cell inserted
    // (consistent with SiYuan's internal merged-cell convention), otherwise the row/column correspondence
    // would get scrambled.
    // Output selects th/td based on the normalized thead/tbody, ensuring the result can be parsed directly as
    // a standalone table block.
    if (outCells.length === 0) {
        return "";
    }
    const maxOutRow = outCells.reduce((m, oc) => Math.max(m, oc.newRow + oc.newRowspan - 1), 0);
    const maxOutCol = outCells.reduce((m, oc) => Math.max(m, oc.newCol + oc.newColspan - 1), 0);
    // coveredSlots[r][c] = this grid position is covered by a merged cell
    const coveredSlots: boolean[][] = [];
    const outGrid: (OutCell | null)[][] = [];
    for (let r = 0; r <= maxOutRow; r++) {
        outGrid.push(new Array(maxOutCol + 1).fill(null));
        coveredSlots.push(new Array(maxOutCol + 1).fill(false));
    }
    // Sort by newRow, newCol before filling, to ensure the starting cell is processed before its placeholders
    outCells.sort((a, b) => a.newRow - b.newRow || a.newCol - b.newCol);
    outCells.forEach(oc => {
        outGrid[oc.newRow][oc.newCol] = oc;
        // Mark positions covered by rowspan/colspan
        for (let dr = 0; dr < oc.newRowspan; dr++) {
            for (let dc = 0; dc < oc.newColspan; dc++) {
                if (dr === 0 && dc === 0) {
                    continue; // The starting cell itself
                }
                const rr = oc.newRow + dr;
                const cc = oc.newCol + dc;
                if (rr <= maxOutRow && cc <= maxOutCol) {
                    coveredSlots[rr][cc] = true;
                }
            }
        }
    });
    // Compute which section each output row belongs to. A standalone table must include a thead; when copying
    // starts from within tbody, the first row and the rows covered by its rowspan are promoted to the header,
    // to avoid a merged cell spanning across thead/tbody.
    const outSection = (outRow: number) => {
        const origRow = selRowStart + outRow;
        return (origRow < sectionOfRow.length && sectionOfRow[origRow]) ? sectionOfRow[origRow] : "tbody";
    };
    let originalHeadRows = 0;
    while (originalHeadRows <= maxOutRow && outSection(originalHeadRows) === "thead") {
        originalHeadRows++;
    }
    const mergedHeadRows = outCells.reduce((max, item) => {
        return item.newRow === 0 ? Math.max(max, item.newRowspan) : max;
    }, 1);
    const headRows = Math.min(maxOutRow + 1, Math.max(originalHeadRows, mergedHeadRows));
    const getOutputSection = (outRow: number) => {
        return outRow < headRows ? "thead" : "tbody";
    };
    const getCellHTML = (cell: HTMLTableCellElement, section: string) => {
        const tagName = section === "thead" ? "th" : "td";
        if (cell.tagName.toLowerCase() === tagName) {
            return cell.outerHTML;
        }
        const outputCell = document.createElement(tagName);
        Array.from(cell.attributes).forEach(attribute => {
            outputCell.setAttribute(attribute.name, attribute.value);
        });
        outputCell.innerHTML = cell.innerHTML;
        return outputCell.outerHTML;
    };
    let html = "<table>";
    let curSection = "";
    for (let r = 0; r <= maxOutRow; r++) {
        const section = getOutputSection(r);
        if (section !== curSection) {
            if (curSection) {
                html += `</${curSection}>`;
            }
            html += `<${section}>`;
            curSection = section;
        }
        html += "<tr>";
        for (let c = 0; c <= maxOutCol; c++) {
            const slot = outGrid[r][c];
            if (slot) {
                html += getCellHTML(slot.newCell, section);
            } else if (coveredSlots[r][c]) {
                // A placeholder covered by rowspan/colspan uses the cell tag matching the current section.
                const tagName = section === "thead" ? "th" : "td";
                html += `<${tagName} class="fn__none"></${tagName}>`;
            } else {
                // A hole within the selection (shouldn't happen in theory), filled with an empty cell for the current section.
                html += section === "thead" ? "<th></th>" : "<td></td>";
            }
        }
        html += "</tr>";
    }
    if (curSection) {
        html += `</${curSection}>`;
    }
    html += "</table>";
    return html;
};
