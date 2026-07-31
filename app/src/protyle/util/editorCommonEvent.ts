import {focusBlock, focusByRange, getRangeByPoint} from "./selection";
import {
    getContenteditableElement,
    getNextBlockSibling,
    getParentBlock,
    getPreviousBlockSibling,
    getSbChildBlockCount,
    getTopAloneElement
} from "../wysiwyg/getBlock";
import {hideCaretLine, hideDragTip, showCaretLine, showDragTip, transparentImgSrc} from "./dragTip";
import {
    hasClosestBlock,
    hasClosestByAttribute,
    hasClosestByClassName,
    hasClosestByTag,
    hasTopClosestByAttribute,
    isInEmbedBlock
} from "./hasClosest";
import {Constants} from "../../constants";
import {paste} from "./paste";
import {
    cancelSB,
    genEmptyElement,
    genSBElement,
    insertEmptyBlock,
    refreshSbAndPersistWidth,
    refreshSbResize
} from "../../block/util";
import {transaction, turnsIntoOneTransaction} from "../wysiwyg/transaction";
import {updateListOrder} from "../wysiwyg/list";
import {fetchPost, fetchSyncPost} from "../../util/fetch";
import {onGet} from "./onGet";
/// #if !MOBILE
import {getAllEditor} from "../../layout/getAll";
import {updatePanelByEditor} from "../../editor/util";
/// #endif
import {blockRender} from "../render/blockRender";
/// #else
import {uploadFiles, uploadLocalFiles} from "../upload";
import {insertHTML} from "./insertHTML";
import {isBrowser} from "../../util/functions";
import {hideElements} from "../ui/hideElements";
import {insertAttrViewBlockAnimation} from "../render/av/row";
import * as dayjs from "dayjs";
import {zoomOut} from "../../menus/protyle";
/// #if !BROWSER
import {webUtils} from "electron";
import {dragUpload} from "../render/av/asset";
/// #endif
import {addDragFill, getTypeByCellElement} from "../render/av/cell";
import {processClonePHElement} from "../render/util";
import {insertGalleryItemAnimation} from "../render/av/gallery/item";
import {clearSelect} from "./clear";
import {dragoverTab} from "../render/av/view";
import {setFold} from "./blockFold";
import {isEncryptedBox} from "../../util/pathName";

const convertListItemSubtype = (listItem: Element, subtype: string) => {
    const actionElement = listItem.querySelector(".protyle-action");
    if (!actionElement || !["u", "o", "t"].includes(subtype)) {
        return;
    }
    if (subtype === "o") {
        actionElement.outerHTML = '<div contenteditable="false" draggable="true" class="protyle-action protyle-action--order">1.</div>';
        listItem.setAttribute("data-marker", "1.");
        listItem.removeAttribute("data-task");
    } else if (subtype === "t") {
        actionElement.outerHTML = '<div class="protyle-action protyle-action--task" draggable="true"><svg><use xlink:href="#iconUncheck"></use></svg></div>';
        listItem.setAttribute("data-marker", "*");
        listItem.setAttribute("data-task", " ");
    } else {
        actionElement.outerHTML = '<div class="protyle-action" draggable="true"><svg><use xlink:href="#iconDot"></use></svg></div>';
        listItem.setAttribute("data-marker", "*");
        listItem.removeAttribute("data-task");
    }
    listItem.setAttribute("data-subtype", subtype);
    listItem.classList.remove("protyle-task--done");
    listItem.setAttribute(Constants.ATTRIBUTE_EDITING, "true");
};

const getTargetListItem = (targetElement: Element, isBottom: boolean) => {
    if (targetElement.classList.contains("li")) {
        return targetElement as HTMLElement;
    }
    if (targetElement.classList.contains("list")) {
        const listItems = targetElement.querySelectorAll(":scope > .li");
        return (isBottom ? listItems[listItems.length - 1] : listItems[0]) as HTMLElement;
    }
    return targetElement.closest(".li") as HTMLElement;
};

// position: afterbegin means dragging to form a superblock; "afterend"/"beforebegin" are ordinary drags
const moveTo = async (protyle: IProtyle, sourceElements: Element[], targetElement: Element,
                      isSameDoc: boolean, position: InsertPosition, isCopy: boolean) => {
    const doOperations: IOperation[] = [];
    const undoOperations: IOperation[] = [];
    const copyFoldHeadingIds: { newId: string, oldId: string }[] = [];
    const targetId = targetElement.getAttribute("data-node-id");
    const newSourceElements: Element[] = [];
    let tempTargetElement = targetElement;
    let isSameLi = true;
    sourceElements.find(item => {
        if (!item.classList.contains("li") || !targetElement.classList.contains("li")) {
            isSameLi = false;
            return true;
        }
    });
    let newListElement: Element;
    let newListId: string;
    const orderListElements: { [key: string]: Element } = {};
    // Explicitly capture each source block's position before the DOM move, for use by undoOperations.
    // Cannot rely on getParentBlock(item) inside the loop below (item's parent has already changed after the
    // move), or undo would move the block to the wrong location.
    // Key point: for a top-level document block, getParentBlock returns the .protyle-wysiwyg container (which
    // has no data-node-id). We can't use the target protyle's rootID here (wrong document in cross-document
    // drags) -- we must use the rootID of the document the source DOM actually belongs to.
    const sourcePositions = new Map<string, { previousID: string, parentID: string }>();
    for (const item of sourceElements) {
        const id = item.getAttribute("data-node-id");
        if (id) {
            const parentBlock = getParentBlock(item);
            let srcParentID = parentBlock?.getAttribute("data-node-id");
            if (!srcParentID) {
                // Top-level block: its parent is the .protyle-wysiwyg container (no data-node-id).
                let srcRootID = "";
                /// #if !MOBILE
                // Use getAllEditor to look up the source protyle that item belongs to, and read its block.rootID.
                const sourceEditor = getAllEditor().find(editor =>
                    editor.protyle.wysiwyg.element === parentBlock);
                if (sourceEditor?.protyle?.block?.rootID) {
                    srcRootID = sourceEditor.protyle.block.rootID;
                }
                /// #endif
                if (srcRootID) {
                    srcParentID = srcRootID;
                } else {
                    // Cross-window or mobile: getAllEditor can't find the source editor, so fall back to the
                    // kernel API to look up the block's real rootID.
                    // Can't fall back to the target protyle's rootID (undo would move the block to the wrong document).
                    const response = await fetchSyncPost("/api/block/getBlockInfo", {id});
                    srcParentID = response?.data?.rootID || "";
                }
            }
            sourcePositions.set(id, {
                previousID: getPreviousBlockSibling(item)?.getAttribute("data-node-id") || "",
                parentID: srcParentID || "",
            });
        }
    }
    for (let index = sourceElements.length - 1; index >= 0; index--) {
        const item = sourceElements[index];
        const originalSubtype = item.getAttribute("data-subtype");
        const id = item.getAttribute("data-node-id");
        const parentID = getParentBlock(item).getAttribute("data-node-id") || protyle.block.parentID || protyle.block.rootID;
        if (item.getAttribute("data-type") === "NodeListItem" && !newListId && !isSameLi) {
            newListId = Lute.NewNodeID();
            newListElement = document.createElement("div");
            newListElement.innerHTML = `<div data-subtype="${item.getAttribute("data-subtype")}" data-node-id="${newListId}" data-type="NodeList" class="list"><div class="protyle-attr" contenteditable="false">${Constants.ZWSP}</div></div>`;
            newListElement = newListElement.firstElementChild;
            doOperations.push({
                action: "insert",
                data: newListElement.outerHTML,
                id: newListId,
                previousID: position === "afterbegin" ? null : (position === "afterend" ? targetId : getPreviousBlockSibling(tempTargetElement)?.getAttribute("data-node-id")),
                parentID: position === "afterbegin" ? targetId : (getParentBlock(tempTargetElement)?.getAttribute("data-node-id") || protyle.block.parentID || protyle.block.rootID),
            });
            undoOperations.push({
                action: "delete",
                id: newListId
            });
            tempTargetElement.insertAdjacentElement(position, newListElement);
            newSourceElements.push(newListElement);
        }
        const copyNewId = Lute.NewNodeID();
        if (isCopy && item.getAttribute("data-type") === "NodeHeading" && item.getAttribute("fold") === "1") {
            copyFoldHeadingIds.push({
                newId: copyNewId,
                oldId: id
            });
        }

        let copyElement;
        if (isCopy) {
            undoOperations.push({
                action: "delete",
                id: copyNewId,
            });
        } else {
            // Build the undo operation from the source position captured before the DOM move, since item's
            // parent/sibling have already changed after the move, which would send undo to the wrong location
            const srcPos = sourcePositions.get(id) || {previousID: "", parentID};
            undoOperations.push({
                action: "move",
                id,
                previousID: srcPos.previousID,
                parentID: srcPos.parentID,
            });
        }
        if (!isSameDoc && !isCopy) {
            // The same document is open twice
            const sameElement = protyle.wysiwyg.element.querySelector(`[data-node-id="${id}"]`);
            if (sameElement) {
                sameElement.remove();
            }
        }
        if (isCopy) {
            copyElement = item.cloneNode(true) as HTMLElement;
            copyElement.setAttribute("data-node-id", copyNewId);
            copyElement.querySelectorAll("[data-node-id]").forEach((e) => {
                const newId = Lute.NewNodeID();
                e.setAttribute("data-node-id", newId);
                e.setAttribute("updated", newId.split("-")[0]);
            });
            const targetSubtype = targetElement.getAttribute("data-subtype");
            if (copyElement.getAttribute("data-type") === "NodeListItem" &&
                targetElement.getAttribute("data-type") === "NodeListItem" && targetSubtype &&
                copyElement.getAttribute("data-subtype") !== targetSubtype) {
                convertListItemSubtype(copyElement, targetSubtype);
            }
            if (newListId) {
                newListElement.insertAdjacentElement("afterbegin", copyElement);
                doOperations.push({
                    action: "insert",
                    id: copyNewId,
                    data: copyElement.outerHTML,
                    parentID: newListId,
                });
            } else {
                tempTargetElement.insertAdjacentElement(position, copyElement);
                doOperations.push({
                    action: "insert",
                    id: copyNewId,
                    data: copyElement.outerHTML,
                    previousID: position === "afterbegin" ? null : (position === "afterend" ? targetId : getPreviousBlockSibling(copyElement)?.getAttribute("data-node-id")), // cannot cache this in a constant, it changes after the move
                    parentID: position === "afterbegin" ? targetId : (getParentBlock(copyElement)?.getAttribute("data-node-id") || protyle.block.parentID || protyle.block.rootID),
                });
                newSourceElements.push(copyElement);
            }
        } else {
            let topSourceElement = getTopAloneElement(item);
            const oldSourceParentElement = getParentBlock(item);
            if (item.classList.contains("li") && item.getAttribute("data-subtype") === "o") {
                orderListElements[item.parentElement.getAttribute("data-node-id")] = item.parentElement;
            }
            if (newListId) {
                newListElement.insertAdjacentElement("afterbegin", item);
                doOperations.push({
                    action: "move",
                    id,
                    parentID: newListId,
                });
            } else {
                tempTargetElement.insertAdjacentElement(position, item);
                doOperations.push({
                    action: "move",
                    id,
                    previousID: position === "afterbegin" ? null : (position === "afterend" ? targetId : getPreviousBlockSibling(item)?.getAttribute("data-node-id")), // cannot cache this in a constant, it changes after the move
                    parentID: position === "afterbegin" ? targetId : (getParentBlock(item)?.getAttribute("data-node-id") || protyle.block.parentID || protyle.block.rootID),
                });
                newSourceElements.push(item);
            }

            if (topSourceElement !== item) {
                if (topSourceElement.contains(item)) {
                    topSourceElement = getTopAloneElement(oldSourceParentElement);
                }
                // Empty element left behind after the drag
                doOperations.push({
                    action: "delete",
                    id: topSourceElement.getAttribute("data-node-id"),
                });
                undoOperations.push({
                    action: "insert",
                    data: topSourceElement.outerHTML,
                    id: topSourceElement.getAttribute("data-node-id"),
                    previousID: getPreviousBlockSibling(topSourceElement)?.getAttribute("data-node-id"),
                    parentID: getParentBlock(topSourceElement)?.getAttribute("data-node-id") || protyle.block.parentID || protyle.block.rootID
                });
                const topSourceParentElement = topSourceElement.parentElement;
                topSourceElement.remove();
                if (!isSameDoc) {
                    // The same document is open twice
                    const sameElement = protyle.wysiwyg.element.querySelector(`[data-node-id="${topSourceElement.getAttribute("data-node-id")}"]`);
                    if (sameElement) {
                        sameElement.remove();
                    }
                }
                if (topSourceParentElement.classList.contains("sb") && getSbChildBlockCount(topSourceParentElement) === 1) {
                    // Only one element remains in the sb after the drag
                    if (isSameDoc) {
                        const sbData = await cancelSB(protyle, topSourceParentElement);
                        doOperations.push(sbData.doOperations[0], sbData.doOperations[1]);
                        undoOperations.push(sbData.undoOperations[1], sbData.undoOperations[0]);
                    } else {
                        /// #if !MOBILE
                        const allEditor = getAllEditor();
                        for (let i = 0; i < allEditor.length; i++) {
                            if (allEditor[i].protyle.element.contains(topSourceParentElement)) {
                                const otherSbData = await cancelSB(allEditor[i].protyle, topSourceParentElement);
                                doOperations.push(otherSbData.doOperations[0], otherSbData.doOperations[1]);
                                undoOperations.push(otherSbData.undoOperations[1], otherSbData.undoOperations[0]);
                                // Cross-document moves are reversible entries in the global undo stack, so there's
                                // no need to clear the source editor's history
                                break;
                            }
                        }
                        /// #endif
                    }
                }
            } else if (oldSourceParentElement.classList.contains("sb") && getSbChildBlockCount(oldSourceParentElement) === 1) {
                // Only one element remains in the sb after the drag
                if (isSameDoc) {
                    const sbData = await cancelSB(protyle, oldSourceParentElement);
                    doOperations.push(sbData.doOperations[0], sbData.doOperations[1]);
                    undoOperations.push(sbData.undoOperations[1], sbData.undoOperations[0]);
                } else {
                    /// #if !MOBILE
                    const allEditor = getAllEditor();
                    for (let i = 0; i < allEditor.length; i++) {
                        if (allEditor[i].protyle.element.contains(oldSourceParentElement)) {
                            const otherSbData = await cancelSB(allEditor[i].protyle, oldSourceParentElement);
                            doOperations.push(otherSbData.doOperations[0], otherSbData.doOperations[1]);
                            undoOperations.push(otherSbData.undoOperations[1], otherSbData.undoOperations[0]);
                            // Cross-document moves are reversible entries in the global undo stack, so there's
                            // no need to clear the source editor's history
                            break;
                        }
                    }
                    /// #endif
                }
            } else if (oldSourceParentElement.classList.contains("protyle-wysiwyg") && oldSourceParentElement.childElementCount === 0) {
                /// #if !MOBILE
                // The root document's original content is empty after the drag
                getAllEditor().find(item => {
                    if (item.protyle.element.contains(oldSourceParentElement)) {
                        if (!item.protyle.block.showAll) {
                            const newId = Lute.NewNodeID();
                            doOperations.splice(0, 0, {
                                action: "insert",
                                id: newId,
                                data: genEmptyElement(false, false, newId).outerHTML,
                                parentID: item.protyle.block.parentID
                            });
                            undoOperations.splice(0, 0, {
                                action: "delete",
                                id: newId,
                            });
                        } else {
                            zoomOut({protyle: item.protyle, id: item.protyle.block.rootID});
                        }
                        return true;
                    }
                });
                /// #endif
            }
        }

        if (!isCopy && item.getAttribute("data-type") === "NodeListItem" && targetElement.getAttribute("data-type") === "NodeListItem") {
            const targetSubtype = targetElement.getAttribute("data-subtype");
            if (targetSubtype && item.getAttribute("data-subtype") !== targetSubtype) {
                const originalHTML = item.outerHTML;
                convertListItemSubtype(item, targetSubtype);
                doOperations.push({
                    action: "update",
                    id,
                    data: item.outerHTML,
                });
                undoOperations.push({
                    action: "update",
                    id,
                    data: originalHTML,
                });
            }
        }

        if (newListId && (index === 0 ||
            sourceElements[index - 1].getAttribute("data-type") !== "NodeListItem" ||
            sourceElements[index - 1].getAttribute("data-subtype") !== originalSubtype)
        ) {
            if (position === "beforebegin") {
                tempTargetElement = newListElement;
            }
            newListId = null;
            if (newListElement.getAttribute("data-subtype") === "o" && newListElement.firstElementChild.getAttribute("data-marker") !== "1.") {
                Array.from(newListElement.children).forEach((listItem) => {
                    if (listItem.classList.contains("protyle-attr")) {
                        return;
                    }
                    undoOperations.push({
                        action: "update",
                        id: listItem.getAttribute("data-node-id"),
                        data: listItem.outerHTML
                    });
                });
                updateListOrder(newListElement, 1);
                Array.from(newListElement.children).forEach((listItem) => {
                    if (listItem.classList.contains("protyle-attr")) {
                        return;
                    }
                    doOperations.push({
                        action: "update",
                        id: listItem.getAttribute("data-node-id"),
                        data: listItem.outerHTML
                    });
                });
                updateListOrder(newListElement, 1);
            }
        } else if (position === "beforebegin") {
            tempTargetElement = isCopy ? copyElement : item;
        }
    }
    Object.keys(orderListElements).forEach(key => {
        Array.from(orderListElements[key].children).forEach((item) => {
            if (item.classList.contains("protyle-attr")) {
                return;
            }
            undoOperations.push({
                action: "update",
                id: item.getAttribute("data-node-id"),
                data: item.outerHTML
            });
        });
        updateListOrder(orderListElements[key], 1);
        Array.from(orderListElements[key].children).forEach((item) => {
            if (item.classList.contains("protyle-attr")) {
                return;
            }
            doOperations.push({
                action: "update",
                id: item.getAttribute("data-node-id"),
                data: item.outerHTML
            });
        });
    });
    undoOperations.reverse();
    for (let j = 0; j < copyFoldHeadingIds.length; j++) {
        const childrenItem = copyFoldHeadingIds[j];
        const responseTransaction = await fetchSyncPost("/api/block/getHeadingInsertTransaction", {id: childrenItem.oldId});
        responseTransaction.data.doOperations.splice(0, 1);
        responseTransaction.data.doOperations[0].previousID = childrenItem.newId;
        responseTransaction.data.undoOperations.splice(0, 1);
        doOperations.push(...responseTransaction.data.doOperations);
        undoOperations.push(...responseTransaction.data.undoOperations);
    }
    return {
        doOperations,
        undoOperations,
        newSourceElements
    };
};

const dragSb = async (protyle: IProtyle, sourceElements: Element[], targetElement: Element, isBottom: boolean,
                      direct: "col" | "row", isCopy: boolean) => {
    const isSameDoc = protyle.element.contains(sourceElements[0]);
    // Record the superblock the source block belongs to before the move, so its handle can be refreshed after
    // the move (it needs rebuilding once the block leaves) https://github.com/siyuan-note/siyuan/issues/9521
    const originSbSet = new Set<Element>();
    sourceElements.forEach(el => {
        const sb = el.closest('[data-type="NodeSuperBlock"]');
        if (sb && sb !== targetElement.closest('[data-type="NodeSuperBlock"]')) {
            // The target itself is not inside this superblock (otherwise it's just an internal SB
            // reorder and doesn't need rebuilding)
            originSbSet.add(sb);
        }
    });
    // Dragging the only list item in a list block to the left side of the list block https://github.com/siyuan-note/siyuan/issues/16315
    if (isSameDoc && sourceElements[0].classList.contains("li") && targetElement === sourceElements[0].parentElement &&
        targetElement.childElementCount === sourceElements.length + 1) {
        const outLiElement = sourceElements.find((element) => {
            if (!targetElement.contains(element)) {
                return true;
            }
        });
        if (!outLiElement) {
            return;
        }
    }
    const undoOperations: IOperation[] = [];
    const targetMoveUndo: IOperation = {
        action: "move",
        context: {
            removeFold: "true"
        },
        id: targetElement.getAttribute("data-node-id"),
        previousID: getPreviousBlockSibling(targetElement)?.getAttribute("data-node-id"),
        parentID: getParentBlock(targetElement)?.getAttribute("data-node-id") || protyle.block.parentID || protyle.block.rootID
    };
    const sbElement = genSBElement(direct);
    targetElement.parentElement.replaceChild(sbElement, targetElement);
    const doOperations: IOperation[] = [{
        action: "insert",
        data: sbElement.outerHTML,
        id: sbElement.getAttribute("data-node-id"),
        nextID: getNextBlockSibling(sbElement)?.getAttribute("data-node-id"),
        previousID: getPreviousBlockSibling(sbElement)?.getAttribute("data-node-id"),
        parentID: getParentBlock(sbElement)?.getAttribute("data-node-id") || protyle.block.parentID || protyle.block.rootID
    }];
    // Insert temporarily to prevent later miscalculation, then correct the position with a final move
    sbElement.lastElementChild.before(targetElement);
    const moveToResult = await moveTo(protyle, sourceElements, sbElement, isSameDoc, "afterbegin", isCopy);
    doOperations.push(...moveToResult.doOperations);
    undoOperations.push(...moveToResult.undoOperations);
    const newSourceParentElement = moveToResult.newSourceElements;
    // Dragging two elements in a row-layout superblock A to form a col-layout superblock B: canceling superblock A
    // would delete targetElement, so we must move it before deleting https://github.com/siyuan-note/siyuan/issues/16292
    let removeIndex = doOperations.length;
    doOperations.find((item, index) => {
        // Dragging two elements in a row-layout superblock A to form a col-layout superblock B: canceling superblock A
        // would delete targetElement, so we must move it before deleting https://github.com/siyuan-note/siyuan/issues/16292
        if (item.action === "delete" && item.id === targetMoveUndo.parentID) {
            removeIndex = index;
        }
        // A superblock with two blocks, dragging one of them outside the superblock https://github.com/siyuan-note/siyuan/issues/16292#issuecomment-3523600155
        if (item.action === "delete" && item.id === targetElement.getAttribute("data-node-id")) {
            targetElement = sbElement.querySelector(`[data-node-id="${doOperations[index - 1].id}"]`);
        }
    });

    if (isBottom) {
        // Dragging below the superblock col, other blocks to the right
        sbElement.insertAdjacentElement("afterbegin", targetElement);
        doOperations.splice(removeIndex, 0, {
            action: "move",
            id: targetElement.getAttribute("data-node-id"),
            parentID: sbElement.getAttribute("data-node-id")
        });
    } else {
        sbElement.lastElementChild.insertAdjacentElement("beforebegin", targetElement);
        doOperations.splice(removeIndex, 0, {
            action: "move",
            id: targetElement.getAttribute("data-node-id"),
            previousID: newSourceParentElement[0].getAttribute("data-node-id"),
        });
    }
    undoOperations.push(targetMoveUndo);
    undoOperations.push({
        action: "delete",
        id: sbElement.getAttribute("data-node-id"),
    });
    const foldElements: Element[] = [];
    newSourceParentElement.forEach(item => {
        const nextBlockElement = getNextBlockSibling(item);
        if (item.getAttribute("data-type") === "NodeHeading" && item.getAttribute("fold") === "1" &&
            nextBlockElement && (
                nextBlockElement.getAttribute("data-type") !== "NodeHeading" ||
                (nextBlockElement.getAttribute("data-subtype") || "") > item.getAttribute("data-subtype")
            )) {
            foldElements.push(item);
        }
    });
    if ((newSourceParentElement.length > 1 || foldElements.length > 0) && direct === "col") {
        const mergeOperations = await turnsIntoOneTransaction({
            protyle,
            selectsElement: newSourceParentElement.reverse(),
            type: "BlocksMergeSuperBlock",
            level: "row",
            unfocus: true,
            getOperations: true
        });
        doOperations.push(...mergeOperations.doOperations);
        undoOperations.splice(0, 0, ...mergeOperations.undoOperations);
    }
    foldElements.forEach(item => {
        const foldOperations = setFold(protyle, item, true, false, false, true);
        doOperations.push(...foldOperations.doOperations);
        undoOperations.splice(0, 0, ...foldOperations.undoOperations);
    });
    refreshSbResize(sbElement);
    originSbSet.forEach(sb => {
        refreshSbAndPersistWidth(sb, doOperations, undoOperations);
    });
    // Cross-document moves are reversible entries: the global undo stack is partitioned per rootID and linked
    // together, and undo shows a confirmation based on mutatedRootIDs
    transaction(protyle, doOperations, undoOperations);
    if (document.contains(sourceElements[0])) {
        focusBlock(sourceElements[0]);
    } else {
        focusBlock(targetElement);
    }
};

const dragSame = async (protyle: IProtyle, sourceElements: Element[], targetElement: Element, isBottom: boolean, isCopy: boolean) => {
    const isSameDoc = protyle.element.contains(sourceElements[0]);
    const doOperations: IOperation[] = [];
    const undoOperations: IOperation[] = [];
    // Record the superblock the source block belongs to before the move, so its handle can be refreshed after
    // the move (it needs rebuilding once the block leaves)
    const originSbSet = new Set<Element>();
    sourceElements.forEach(el => {
        const sb = el.closest('[data-type="NodeSuperBlock"]');
        if (sb) {
            originSbSet.add(sb);
        }
    });

    const moveToResult = await moveTo(protyle, sourceElements, targetElement, isSameDoc, isBottom ? "afterend" : "beforebegin", isCopy);
    doOperations.push(...moveToResult.doOperations);
    undoOperations.push(...moveToResult.undoOperations);
    const newSourceParentElement = moveToResult.newSourceElements;
    let foldData;
    const previousBlockElement = getPreviousBlockSibling(targetElement);
    if (isBottom &&
        targetElement.getAttribute("data-type") === "NodeHeading" &&
        targetElement.getAttribute("fold") === "1") {
        foldData = setFold(protyle, targetElement, true, false, false, true);
    } else if (!isBottom &&
        previousBlockElement?.getAttribute("data-type") === "NodeHeading" &&
        previousBlockElement.getAttribute("fold") === "1") {
        foldData = setFold(protyle, previousBlockElement, true, false, false, true);
    }
    if (foldData) {
        foldData.doOperations[0].context = {
            focusId: sourceElements[0].getAttribute("data-node-id"),
        };
        doOperations.push(...foldData.doOperations);
        undoOperations.push(...foldData.undoOperations);
    }
    if (targetElement.getAttribute("data-type") === "NodeListItem" &&
        targetElement.getAttribute("data-subtype") === "o") {
        // https://github.com/siyuan-note/insider/issues/536
        Array.from(targetElement.parentElement.children).forEach((item) => {
            if (item.classList.contains("protyle-attr")) {
                return;
            }
            undoOperations.splice(0, 0, {
                action: "update",
                id: item.getAttribute("data-node-id"),
                data: item.outerHTML
            });
        });
        updateListOrder(targetElement.parentElement, 1);
        Array.from(targetElement.parentElement.children).forEach((item) => {
            if (item.classList.contains("protyle-attr")) {
                return;
            }
            doOperations.push({
                action: "update",
                id: item.getAttribute("data-node-id"),
                data: item.outerHTML
            });
        });
    }
    let hasFoldHeading = false;
    newSourceParentElement.forEach(item => {
        if (item.getAttribute("data-type") === "NodeHeading" && item.getAttribute("fold") === "1") {
            hasFoldHeading = true;
            const nextBlockElement = getNextBlockSibling(item);
            if (nextBlockElement && (
                nextBlockElement.getAttribute("data-type") !== "NodeHeading" ||
                nextBlockElement.getAttribute("data-subtype") > item.getAttribute("data-subtype")
            )) {
                const foldOperations = setFold(protyle, item, true, false, false, true);
                doOperations.push(...foldOperations.doOperations);
                // Don't fold, otherwise it can't be undone -- undoOperations.push(...foldOperations.undoOperations);
            }
            return true;
        }
    });
    // After moving into/out of a superblock, refresh the drag handles and redistribute widths (e.g. dragging A
    // in front of B inside a superblock requires adding a handle between A and B)
    const dragSbSet = new Set<Element>(originSbSet);
    [newSourceParentElement[0], targetElement].forEach(el => {
        const sb = el?.closest('[data-type="NodeSuperBlock"]');
        if (sb) {
            dragSbSet.add(sb);
        }
    });
    dragSbSet.forEach(sb => {
        refreshSbAndPersistWidth(sb, doOperations, undoOperations);
    });
    if ((newSourceParentElement.length > 1 || hasFoldHeading) &&
        newSourceParentElement[0].parentElement.classList.contains("sb") &&
        newSourceParentElement[0].parentElement.getAttribute("data-sb-layout") === "col") {
        // Merge into the same transaction, otherwise the new superblock id wouldn't be found in a second transaction
        const mergeOperations = await turnsIntoOneTransaction({
            protyle,
            selectsElement: newSourceParentElement.reverse(),
            type: "BlocksMergeSuperBlock",
            level: "row",
            unfocus: true,
            getOperations: true
        });
        doOperations.push(...mergeOperations.doOperations);
        undoOperations.splice(0, 0, ...mergeOperations.undoOperations);
    }
    // Cross-document moves are reversible entries: the global undo stack is partitioned per rootID and linked
    // together, and undo shows a confirmation based on mutatedRootIDs
    transaction(protyle, doOperations, undoOperations);
    if (document.contains(sourceElements[0])) {
        focusBlock(sourceElements[0]);
    } else {
        focusBlock(targetElement);
    }
};

export const dropEvent = (protyle: IProtyle, editorElement: HTMLElement) => {
    editorElement.addEventListener("dragstart", (event) => {
        if (protyle.disabled) {
            event.preventDefault();
            event.stopPropagation();
            return;
        }
        let target = event.target as HTMLElement;
        if (target.classList?.contains("av__gallery-img")) {
            target = hasClosestByClassName(target, "av__gallery-item") as HTMLElement;
        }
        if (!target) {
            return;
        }
        if (target.tagName === "IMG") {
            window.siyuan.dragElement = undefined;
            event.preventDefault();
            return;
        }

        if (target.classList) {
            if (hasClosestByClassName(target, "protyle-wysiwyg__embed")) {
                window.siyuan.dragElement = undefined;
                event.preventDefault();
            } else if (target.parentElement.parentElement.classList.contains("av__views")) {
                window.siyuan.dragElement = target;
                target.style.width = target.clientWidth + "px";
                target.style.opacity = ".36";
                event.dataTransfer.setData(`${Constants.SIYUAN_DROP_GUTTER}NodeAttributeView${Constants.ZWSP}ViewTab${Constants.ZWSP}${[target.previousElementSibling?.getAttribute("data-id")]}`,
                    target.outerHTML);
                return;
            } else if (target.classList.contains("protyle-action")) {
                target.parentElement.classList.add("protyle-wysiwyg--select");
                const ghostElement = document.createElement("div");
                ghostElement.className = protyle.wysiwyg.element.className;
                const cloneElement = processClonePHElement(target.parentElement.cloneNode(true) as Element);
                cloneElement.querySelectorAll(".iframe").forEach(item => {
                    item.remove();
                });
                ghostElement.append(cloneElement);
                ghostElement.setAttribute("style", `position:fixed;opacity:.1;width:${target.parentElement.clientWidth}px;padding:0;`);
                document.body.append(ghostElement);
                if (window.siyuan.touchDragActive) {
                    // On touch, keep the DOM ghost around so touchDragBridge can follow the finger
                    event.dataTransfer.setDragImage(ghostElement, 0, 0);
                    window.siyuan.touchDragGhost = ghostElement;
                } else {
                    // On desktop, hide the native ghost and use a custom two-zone follower box instead
                    const transparentImg = new Image();
                    transparentImg.src = transparentImgSrc;
                    event.dataTransfer.setDragImage(transparentImg, 0, 0);
                    setTimeout(() => {
                        ghostElement.remove();
                    });
                }
                window.siyuan.dragTitle = getContenteditableElement(target.parentElement)?.textContent?.trim() || "";

                window.siyuan.dragElement = protyle.wysiwyg.element;
                event.dataTransfer.setData(`${Constants.SIYUAN_DROP_GUTTER}NodeListItem${Constants.ZWSP}${target.parentElement.getAttribute("data-subtype")}${Constants.ZWSP}${[target.parentElement.getAttribute("data-node-id")]}`,
                    protyle.wysiwyg.element.innerHTML);
                return;
            } else if (target.classList.contains("av__cell--header")) {
                window.siyuan.dragElement = target;
                event.dataTransfer.setData(`${Constants.SIYUAN_DROP_GUTTER}NodeAttributeView${Constants.ZWSP}Col${Constants.ZWSP}${[target.getAttribute("data-col-id")]}`,
                    target.outerHTML);
                return;
            } else if (target.classList.contains("av__gallery-item")) {
                const blockElement = hasClosestBlock(target);
                if (blockElement) {
                    if (blockElement.querySelector('.block__icon[data-type="av-sort"]')?.classList.contains("block__icon--active")) {
                        const bodyElements = blockElement.querySelectorAll(".av__body");
                        if (bodyElements.length === 1) {
                            event.preventDefault();
                            event.stopPropagation();
                            return;
                        } else if (["template", "created", "updated"].includes(bodyElements[0].getAttribute("data-dtype"))) {
                            event.preventDefault();
                            event.stopPropagation();
                            return;
                        }
                    }
                    if (!target.classList.contains("av__gallery-item--select")) {
                        blockElement.querySelectorAll(".av__gallery-item--select").forEach(item => {
                            item.classList.remove("av__gallery-item--select");
                        });
                        target.classList.add("av__gallery-item--select");
                    }
                    const ghostElement = document.createElement("div");
                    ghostElement.className = "protyle-wysiwyg protyle-wysiwyg--attr";
                    const isKanban = blockElement.getAttribute("data-av-type") === "kanban";
                    if (isKanban) {
                        ghostElement.innerHTML = `<div class="${blockElement.querySelector(".av__kanban").className}"></div>`;
                    }
                    let galleryElement: HTMLElement;
                    let cloneGalleryElement = document.createElement("div");
                    const selectElements = blockElement.querySelectorAll(".av__gallery-item--select");
                    selectElements.forEach(item => {
                        if (!galleryElement || !galleryElement.contains(item)) {
                            galleryElement = item.parentElement;
                            cloneGalleryElement = document.createElement("div");
                            if (isKanban) {
                                cloneGalleryElement.className = "av__kanban-group";
                                cloneGalleryElement.setAttribute("style", item.parentElement.parentElement.parentElement.getAttribute("style"));
                                cloneGalleryElement.innerHTML = '<div class="av__gallery"></div>';
                                ghostElement.firstElementChild.appendChild(cloneGalleryElement);
                            } else {
                                cloneGalleryElement.classList.add("av__gallery");
                                cloneGalleryElement.setAttribute("style", `width: 100vw;margin-bottom: 16px;grid-template-columns: repeat(auto-fill, ${selectElements[0].clientWidth}px);`);
                                ghostElement.appendChild(cloneGalleryElement);
                            }
                        }
                        const cloneItem = processClonePHElement(item.cloneNode(true) as Element);
                        cloneItem.setAttribute("style", `height:${item.clientHeight}px;`);
                        cloneItem.classList.remove("av__gallery-item--select");
                        if (isKanban) {
                            cloneGalleryElement.firstElementChild.appendChild(cloneItem);
                        } else {
                            cloneGalleryElement.appendChild(cloneItem);
                        }
                    });
                    ghostElement.setAttribute("style", "left: 1px;top:100vh;position:fixed;opacity:.1;padding:0;z-index: 8");
                    document.body.append(ghostElement);
                    event.dataTransfer.setDragImage(ghostElement, -10, -10);
                    if (window.siyuan.touchDragActive) {
                        window.siyuan.touchDragGhost = ghostElement;
                    } else {
                        setTimeout(() => {
                            ghostElement.remove();
                        });
                    }
                    window.siyuan.dragElement = target;
                    const selectIds: string[] = [];
                    blockElement.querySelectorAll(".av__gallery-item--select").forEach(item => {
                        const bodyElement = hasClosestByClassName(item, "av__body") as HTMLElement;
                        const groupId = bodyElement.getAttribute("data-group-id");
                        selectIds.push(item.getAttribute("data-id") + (groupId ? `@${groupId}` : ""));
                    });
                    event.dataTransfer.setData(`${Constants.SIYUAN_DROP_GUTTER}NodeAttributeView${Constants.ZWSP}GalleryItem${Constants.ZWSP}${selectIds}`,
                        ghostElement.outerHTML);
                }
                return;
            }
        }
        // Selected text within the editor is being dragged
        event.dataTransfer.setData(Constants.SIYUAN_DROP_EDITOR, Constants.SIYUAN_DROP_EDITOR);
        protyle.element.style.userSelect = "auto";
        document.onmousemove = null;
        document.onmouseup = null;
    });
    const insertBlockRefs = async (ids: string[]) => {
        let html = "";
        for (const id of ids) {
            const response = await fetchSyncPost("/api/block/getRefText", {id});
            html += protyle.lute.Md2BlockDOM(`((${id} '${response.data}'))`);
        }
        insertHTML(html, protyle);
    };
    const focusBlockRefDrop = (event: DragEvent) => {
        if (event.y > protyle.wysiwyg.element.lastElementChild.getBoundingClientRect().bottom) {
            insertEmptyBlock(protyle, "afterend", protyle.wysiwyg.element.lastElementChild.getAttribute("data-node-id"));
            return true;
        }
        const range = getRangeByPoint(event.clientX, event.clientY);
        if (!range || hasClosestByAttribute(range.startContainer, "data-type", "NodeBlockQueryEmbed")) {
            return false;
        }
        focusByRange(range);
        return true;
    };
    const renderBlockRefDragover = (event: DragEvent) => {
        cleanupDragIndicators(editorElement);
        editorElement.querySelectorAll("[select-start], [select-end]").forEach((item: HTMLElement) => {
            item.removeAttribute("select-start");
            item.removeAttribute("select-end");
        });
        if (event.y <= protyle.wysiwyg.element.lastElementChild.getBoundingClientRect().bottom) {
            const range = getRangeByPoint(event.clientX, event.clientY);
            if (range && !hasClosestByAttribute(range.startContainer, "data-type", "NodeBlockQueryEmbed")) {
                const rect = range.getBoundingClientRect();
                if (rect.height > 0) {
                    showCaretLine(rect.left, rect.top, rect.height);
                }
            }
        } else {
            hideCaretLine();
            const lastBlock = protyle.wysiwyg.element.lastElementChild as HTMLElement;
            if (lastBlock?.hasAttribute("data-node-id")) {
                lastBlock.classList.add("dragover__bottom");
            }
        }
        event.preventDefault();
    };
    editorElement.addEventListener("drop", async (event: DragEvent & { target: HTMLElement }) => {
        // Lite mode doesn't persist to disk, so dragging a block forces copy semantics (a move would delete
        // the source block).
        const isCopyDrag = protyle.lite || event.ctrlKey;
        counter = 0;
        hideDragTip();
        window.siyuan.dragTitle = "";
        if (protyle.disabled || event.dataTransfer.getData(Constants.SIYUAN_DROP_EDITOR)) {
            // Read-only mode / dragging selected text within the editor
            event.preventDefault();
            event.stopPropagation();
            return;
        }
        if (event.dataTransfer.types.includes(Constants.SIYUAN_DROP_BLOCK_REF)) {
            event.preventDefault();
            event.stopPropagation();
            let ids: string[] = [];
            try {
                const data = JSON.parse(event.dataTransfer.getData(Constants.SIYUAN_DROP_BLOCK_REF));
                if (data.workspaceDir?.toLowerCase() !== window.siyuan.config.system.workspaceDir.toLowerCase()) {
                    cleanupDragIndicators(editorElement);
                    return;
                }
                ids = Array.from(new Set((Array.isArray(data.ids) ? data.ids : [])
                    .filter((id: unknown): id is string => typeof id === "string" && /^\d{14}-[0-9a-z]{7}$/.test(id))));
            } catch (e) {
                console.warn("parse block reference drop data failed", e);
            }
            if (ids.length === 0 || hasClosestByClassName(event.target, "av") || !focusBlockRefDrop(event)) {
                cleanupDragIndicators(editorElement);
                return;
            }
            await insertBlockRefs(ids);
            cleanupDragIndicators(editorElement);
            return;
        }
        let gutterType = "";
        for (const type of event.dataTransfer.types) {
            if (type.startsWith(Constants.SIYUAN_DROP_GUTTER)) {
                gutterType = type;
            }
        }
        if (gutterType.startsWith(`${Constants.SIYUAN_DROP_GUTTER}NodeAttributeView${Constants.ZWSP}ViewTab${Constants.ZWSP}`.toLowerCase())) {
            const blockElement = hasClosestBlock(window.siyuan.dragElement);
            if (blockElement) {
                const avID = blockElement.getAttribute("data-av-id");
                const blockID = blockElement.getAttribute("data-node-id");
                const id = window.siyuan.dragElement.getAttribute("data-id");
                transaction(protyle, [{
                    action: "sortAttrViewView",
                    avID,
                    blockID,
                    id,
                    previousID: window.siyuan.dragElement.previousElementSibling?.getAttribute("data-id"),
                    data: "unRefresh"   // no need to re-render
                }], [{
                    action: "sortAttrViewView",
                    avID,
                    blockID,
                    id,
                    previousID: gutterType.split(Constants.ZWSP).pop()
                }]);
            }
            return;
        }
        let targetElement = editorElement.querySelector(".dragover__left, .dragover__right, .dragover__bottom, .dragover__top, .dragover__bottom--sibling, .dragover__top--sibling, .dragover__bottom--child, .dragover__top--child");
        if (targetElement) {
            targetElement.classList.remove("dragover");
            targetElement.removeAttribute("select-start");
            targetElement.removeAttribute("select-end");
        }
        if (gutterType) {
            // Dragging from the gutter or the backlink panel
            const sourceElements: Element[] = [];
            const gutterTypes = gutterType.replace(Constants.SIYUAN_DROP_GUTTER, "").split(Constants.ZWSP);
            const selectedIds = gutterTypes[2].split(",");
            if (event.altKey || event.shiftKey) {
                if (event.y > protyle.wysiwyg.element.lastElementChild.getBoundingClientRect().bottom) {
                    insertEmptyBlock(protyle, "afterend", protyle.wysiwyg.element.lastElementChild.getAttribute("data-node-id"));
                } else {
                    const range = getRangeByPoint(event.clientX, event.clientY);
                    if (hasClosestByAttribute(range.startContainer, "data-type", "NodeBlockQueryEmbed")) {
                        return;
                    } else {
                        focusByRange(range);
                    }
                }
            }
            if (event.altKey || (event.shiftKey && protyle.lite)) {
                // Reference: getRefText -> Md2BlockDOM((id 'text'))
                // In lite mode, Shift (which normally makes an embed block) also goes through the reference
                // path, to avoid depending on embed blocks that require a backend SQL query.
                await insertBlockRefs(selectedIds);
            } else if (event.shiftKey) {
                let html = "";
                selectedIds.forEach(item => {
                    html += `{{select * from blocks where id='${item}'}}\n`;
                });
                insertHTML(protyle.lute.SpinBlockDOM(html), protyle, true);
                blockRender(protyle, protyle.wysiwyg.element);
            } else if (targetElement && targetElement.className.indexOf("dragover__") > -1) {
                let queryClass = "";
                selectedIds.forEach(item => {
                    queryClass += `[data-node-id="${item}"],`;
                });
                if (window.siyuan.dragElement) {
                    window.siyuan.dragElement.querySelectorAll(queryClass.substring(0, queryClass.length - 1)).forEach(elementItem => {
                        if (!isInEmbedBlock(elementItem)) {
                            sourceElements.push(elementItem);
                        }
                    });
                } else if (window.siyuan.config.system.workspaceDir.toLowerCase() === gutterTypes[3]) {
                    // Cross-window drag
                    // Dragging across workspaces is not allowed https://github.com/siyuan-note/siyuan/issues/13582
                    const targetProtyleElement = document.createElement("template");
                    targetProtyleElement.innerHTML = `<div>${event.dataTransfer.getData(gutterType)}</div>`;
                    targetProtyleElement.content.querySelectorAll(queryClass.substring(0, queryClass.length - 1)).forEach(elementItem => {
                        if (!isInEmbedBlock(elementItem)) {
                            sourceElements.push(elementItem);
                        }
                    });
                }

                const sourceIds: string [] = [];
                const srcs: IOperationSrcs[] = [];
                sourceElements.forEach(item => {
                    item.classList.remove("protyle-wysiwyg--hl");
                    item.removeAttribute("select-start");
                    item.removeAttribute("select-end");
                    // Backlink mentions carry a highlight, which should be removed when dragged into the document body
                    item.querySelectorAll('[data-type="search-mark"]').forEach(markItem => {
                        markItem.outerHTML = markItem.innerHTML;
                    });
                    const id = item.getAttribute("data-node-id");
                    sourceIds.push(id);
                    srcs.push({
                        itemID: Lute.NewNodeID(),
                        id,
                        isDetached: false,
                    });
                });

                hideElements(["gutter"], protyle);

                const targetClass = targetElement.className.split(" ");
                targetElement.classList.remove("dragover__bottom", "dragover__top", "dragover__left", "dragover__right",
                    "dragover__bottom--sibling", "dragover__top--sibling", "dragover__bottom--child", "dragover__top--child");
                (targetElement as HTMLElement).style.removeProperty("--drag-indent");
                (targetElement as HTMLElement).style.removeProperty("--drag-guides");
                (targetElement as HTMLElement).style.removeProperty("--drag-line-left");
                (targetElement as HTMLElement).style.removeProperty("--drag-base-bg");
                (targetElement as HTMLElement).style.removeProperty("--drag-base-bg");

                if (targetElement.classList.contains("av__cell")) {
                    const blockElement = hasClosestBlock(targetElement);
                    if (blockElement) {
                        const avID = blockElement.getAttribute("data-av-id");
                        let previousID = "";
                        if (targetClass.includes("dragover__left")) {
                            if (targetElement.previousElementSibling) {
                                if (targetElement.previousElementSibling.classList.contains("av__colsticky")) {
                                    previousID = targetElement.previousElementSibling.lastElementChild.getAttribute("data-col-id");
                                } else {
                                    previousID = targetElement.previousElementSibling.getAttribute("data-col-id");
                                }
                            }
                        } else {
                            previousID = targetElement.getAttribute("data-col-id");
                        }
                        let oldPreviousID = "";
                        const rowElement = hasClosestByClassName(targetElement, "av__row");
                        if (rowElement) {
                            const oldPreviousElement = rowElement.querySelector(`[data-col-id="${gutterTypes[2]}"`)?.previousElementSibling;
                            if (oldPreviousElement) {
                                if (oldPreviousElement.classList.contains("av__colsticky")) {
                                    oldPreviousID = oldPreviousElement.lastElementChild.getAttribute("data-col-id");
                                } else {
                                    oldPreviousID = oldPreviousElement.getAttribute("data-col-id");
                                }
                            }
                        }
                        if (previousID !== oldPreviousID && previousID !== gutterTypes[2]) {
                            transaction(protyle, [{
                                action: "sortAttrViewCol",
                                avID,
                                previousID,
                                id: gutterTypes[2],
                                blockID: blockElement.dataset.nodeId,
                            }], [{
                                action: "sortAttrViewCol",
                                avID,
                                previousID: oldPreviousID,
                                id: gutterTypes[2],
                                blockID: blockElement.dataset.nodeId,
                            }]);
                        }
                    }
                } else if (targetElement.classList.contains("av__row")) {
                    // Dragging into an attribute-view table
                    const blockElement = hasClosestBlock(targetElement);
                    if (blockElement) {
                        let previousID = "";
                        if (targetClass.includes("dragover__bottom")) {
                            previousID = targetElement.getAttribute("data-id") || "";
                        } else {
                            previousID = targetElement.previousElementSibling?.getAttribute("data-id") || "";
                        }
                        const avID = blockElement.getAttribute("data-av-id");
                        if (gutterTypes[0] === "nodeattributeviewrowmenu") {
                            // Dragging within a row
                            const doOperations: IOperation[] = [];
                            const undoOperations: IOperation[] = [];
                            const targetGroupID = targetElement.parentElement.getAttribute("data-group-id");
                            selectedIds.reverse().forEach(item => {
                                const items = item.split("@");
                                const id = items[0];
                                const groupID = items[1] || "";
                                const undoPreviousId = blockElement.querySelector(`.av__body${groupID ? `[data-group-id="${groupID}"]` : ""} .av__row[data-id="${id}"]`).previousElementSibling?.getAttribute("data-id") || "";
                                if (previousID !== id && undoPreviousId !== previousID || (
                                    (undoPreviousId === "" && previousID === "" && targetGroupID !== groupID)
                                )) {
                                    doOperations.push({
                                        action: "sortAttrViewRow",
                                        avID,
                                        previousID,
                                        id,
                                        blockID: blockElement.dataset.nodeId,
                                        groupID,
                                        targetGroupID,
                                    });
                                    undoOperations.push({
                                        action: "sortAttrViewRow",
                                        avID,
                                        previousID: undoPreviousId,
                                        id,
                                        blockID: blockElement.dataset.nodeId,
                                        groupID: targetGroupID,
                                        targetGroupID: groupID,
                                    });
                                }
                            });
                            transaction(protyle, doOperations, undoOperations);
                        } else {
                            const newUpdated = dayjs().format("YYYYMMDDHHmmss");
                            const bodyElement = hasClosestByClassName(targetElement, "av__body");
                            const groupID = bodyElement && bodyElement.getAttribute("data-group-id");
                            transaction(protyle, [{
                                action: "insertAttrViewBlock",
                                avID,
                                previousID,
                                srcs,
                                blockID: blockElement.dataset.nodeId,
                                groupID
                            }, {
                                action: "doUpdateUpdated",
                                id: blockElement.dataset.nodeId,
                                data: newUpdated,
                            }], [{
                                action: "removeAttrViewBlock",
                                srcIDs: sourceIds,
                                avID,
                            }, {
                                action: "doUpdateUpdated",
                                id: blockElement.dataset.nodeId,
                                data: blockElement.getAttribute("updated")
                            }]);
                            blockElement.setAttribute("updated", newUpdated);
                            insertAttrViewBlockAnimation({
                                protyle,
                                blockElement,
                                srcIDs: sourceIds,
                                previousId: previousID,
                                groupID
                            });
                        }
                    }
                } else if (targetElement.classList.contains("av__gallery-item") || targetElement.classList.contains("av__gallery-add")) {
                    // Dragging into an attribute-view gallery
                    const blockElement = hasClosestBlock(targetElement);
                    if (blockElement) {
                        let previousID = "";
                        if (targetClass.includes("dragover__right") || targetClass.includes("dragover__bottom")) {
                            previousID = targetElement.getAttribute("data-id") || "";
                        } else if (targetClass.includes("dragover__top") || targetClass.includes("dragover__left")) {
                            previousID = targetElement.previousElementSibling?.getAttribute("data-id") || "";
                        }
                        const avID = blockElement.getAttribute("data-av-id");
                        if (gutterTypes[1] === "galleryitem" && gutterTypes[0] === "nodeattributeview") {
                            // Dragging within gallery items
                            const doOperations: IOperation[] = [];
                            const undoOperations: IOperation[] = [];
                            const targetGroupID = targetElement.parentElement.parentElement.getAttribute("data-group-id");
                            selectedIds.reverse().forEach(item => {
                                const items = item.split("@");
                                const id = items[0];
                                const groupID = items[1] || "";
                                const undoPreviousId = blockElement.querySelector(`.av__body[data-group-id="${groupID}"] .av__gallery-item[data-id="${id}"]`).previousElementSibling?.getAttribute("data-id") || "";
                                if (previousID !== item && undoPreviousId !== previousID || (
                                    (undoPreviousId === "" && previousID === "" && targetGroupID !== groupID)
                                )) {
                                    doOperations.push({
                                        action: "sortAttrViewRow",
                                        avID,
                                        previousID,
                                        id,
                                        blockID: blockElement.dataset.nodeId,
                                        groupID,
                                        targetGroupID,
                                    });
                                    undoOperations.push({
                                        action: "sortAttrViewRow",
                                        avID,
                                        previousID: undoPreviousId,
                                        id,
                                        blockID: blockElement.dataset.nodeId,
                                        groupID: targetGroupID,
                                        targetGroupID: groupID,
                                    });
                                }
                            });
                            transaction(protyle, doOperations, undoOperations);
                        } else {
                            const newUpdated = dayjs().format("YYYYMMDDHHmmss");
                            const bodyElement = hasClosestByClassName(targetElement, "av__body");
                            transaction(protyle, [{
                                action: "insertAttrViewBlock",
                                avID,
                                previousID,
                                srcs,
                                blockID: blockElement.dataset.nodeId,
                                groupID: bodyElement && bodyElement.getAttribute("data-group-id")
                            }, {
                                action: "doUpdateUpdated",
                                id: blockElement.dataset.nodeId,
                                data: newUpdated,
                            }], [{
                                action: "removeAttrViewBlock",
                                srcIDs: sourceIds,
                                avID,
                            }, {
                                action: "doUpdateUpdated",
                                id: blockElement.dataset.nodeId,
                                data: blockElement.getAttribute("updated")
                            }]);
                            blockElement.setAttribute("updated", newUpdated);
                            insertGalleryItemAnimation({
                                protyle,
                                blockElement,
                                srcIDs: sourceIds,
                                previousId: previousID,
                                groupID: targetElement.parentElement.getAttribute("data-group-id")
                            });
                        }
                    }
                } else if (sourceElements.length > 0) {
                    const isChild = targetClass.some((c: string) => c.indexOf("--child") > -1);
                    const isBottom = targetClass.some((c: string) => c.indexOf("dragover__bottom") === 0);

                    // No-op when a list item/list block is dropped on itself, its descendants, or its original
                    // position, so the source isn't moved out and left as a standalone list
                    const isListItemSource = gutterTypes[0] === "nodelistitem" || gutterTypes[0] === "nodelist";
                    if (isListItemSource) {
                        // No-op when the source list item is inside the target list container (a nested list
                        // item dragged to the top/bottom of its parent list)
                        if (targetElement.classList.contains("list") &&
                            sourceElements.some(s => targetElement.contains(s))) {
                            dragoverElement = undefined;
                            return;
                        }
                        // targetElement may be a list item's child block (e.g. .p) or the list container (.list);
                        // find its corresponding .li before deciding
                        const targetLi = getTargetListItem(targetElement, isBottom);
                        if (targetLi) {
                            const isNoOpDrop = sourceElements.some(source =>
                                source === targetLi ||                                              // dropped on itself
                                source.contains(targetLi) ||                                        // dropped on a descendant
                                (!isChild && isBottom && source === targetLi.nextElementSibling) ||  // sibling below: source is already right after the target
                                (!isChild && !isBottom && source === targetLi.previousElementSibling)); // sibling above: source is already right before the target
                            // Ctrl (copy)/Shift (embed)/Alt (reference) go through their own drop branches and don't
                            // hit this move no-op guard; only a plain move needs to be blocked here
                            if (isNoOpDrop && !event.ctrlKey && !event.shiftKey && !event.altKey) {
                                dragoverElement = undefined;
                                return;
                            }
                        } else {
                            // No-op when a list item/list block is dropped on an adjacent block just outside the
                            // list, or on the parent list (including multi-level nesting), so the source isn't
                            // moved out and left as a standalone list
                            const sourceSelected = sourceElements[0];
                            if (sourceSelected && (sourceSelected.classList.contains("li") || sourceSelected.classList.contains("list"))) {
                                // No-op when the source is inside the target list container
                                if (targetElement.classList.contains("list") && targetElement.contains(sourceSelected)) {
                                    dragoverElement = undefined;
                                    return;
                                }
                                let current: Element = sourceSelected;
                                while (current && current !== editorElement) {
                                    if (current.classList.contains("list") || current.classList.contains("li")) {
                                        const checkSiblings = (container: Element) => {
                                            let prevSibling = container.previousElementSibling;
                                            while (prevSibling && prevSibling.classList.contains("protyle-attr")) {
                                                prevSibling = prevSibling.previousElementSibling;
                                            }
                                            let nextSibling = container.nextElementSibling;
                                            while (nextSibling && nextSibling.classList.contains("protyle-attr")) {
                                                nextSibling = nextSibling.nextElementSibling;
                                            }
                                            return targetElement === prevSibling || targetElement === nextSibling;
                                        };
                                        if (checkSiblings(current)) {
                                            // When the source list itself is a top-level document block and the
                                            // target is its top-level adjacent block, this is a legitimate
                                            // top-level reorder (moveTo will create a new valid list wrapper for
                                            // the new position), so don't block it
                                            if (current.parentElement === editorElement) {
                                                break;
                                            }
                                            dragoverElement = undefined;
                                            return;
                                        }
                                    }
                                    current = current.parentElement;
                                }
                            }
                        }
                    }

                    // Dragging an entire list block (NodeList) onto a list item: expand it into its list items,
                    // to avoid forming an illegal list>list nesting.
                    // But when the target is a list block inside a superblock (col layout), the list block itself
                    // is a column unit of the superblock, so it should go through column reordering (dragSame)
                    // instead of being expanded -- otherwise targetElement gets rewritten to .li and the column
                    // reorder branch is never hit
                    const isColSbChildList = targetElement.parentElement?.getAttribute("data-type") === "NodeSuperBlock" &&
                        targetElement.parentElement?.getAttribute("data-sb-layout") === "col";
                    if (isListItemSource && targetElement.classList.contains("list") &&
                        !(gutterTypes[0] === "nodelist" && isColSbChildList)) {
                        const targetListItem = getTargetListItem(targetElement, isBottom);
                        if (targetListItem) {
                            targetElement = targetListItem;
                        }
                    }

                    if (targetElement.getAttribute("data-type") === "NodeListItem") {
                        const expandedElements: Element[] = [];
                        sourceElements.forEach(item => {
                            if (item.getAttribute("data-type") === "NodeList") {
                                Array.from(item.children).forEach((li) => {
                                    if (li.classList.contains("li")) {
                                        expandedElements.push(li);
                                    }
                                });
                            } else {
                                expandedElements.push(item);
                            }
                        });
                        if (expandedElements.length > 0) {
                            sourceElements.length = 0;
                            sourceElements.push(...expandedElements);
                        }
                    }
                    const hasContentBlockSource = sourceElements.some(item =>
                        !["NodeList", "NodeListItem"].includes(item.getAttribute("data-type")));

                    // A non-list-item source (e.g. a paragraph) dropped in the gap above a nested list's first item:
                    // a list can only contain list items, so a paragraph can't become a sibling of .li. The real
                    // meaning of this gap is "insert at the end of the parent list item's content, before the
                    // nested list", so we retarget the anchor to the parent list item and insert the paragraph as
                    // parent-list-item content before the nested list. Return immediately once handled here to
                    // avoid the generic branch below moving it again.
                    if (hasContentBlockSource && !isChild && targetElement.getAttribute("data-type") === "NodeListItem") {
                        const parentLi = targetElement.parentElement?.parentElement;
                        if (targetClass.some((c: string) => c.indexOf("dragover__top--sibling") === 0) &&
                            parentLi?.classList.contains("li")) {
                            const contentLi = parentLi as HTMLElement;
                            const contentBlocks = Array.from(contentLi.children).filter(
                                c => c.hasAttribute("data-node-id") && !c.classList.contains("list"));
                            const anchorBlock = contentBlocks.length > 0 ? contentBlocks[contentBlocks.length - 1] : null;
                            if (anchorBlock) {
                                // Insert after the last content block: moveTo will place the paragraph before the
                                // nested list, making it part of the list item's content
                                await dragSame(protyle, sourceElements, anchorBlock, true, isCopyDrag);
                            } else {
                                await dragSame(protyle, sourceElements, contentLi, isBottom, isCopyDrag);
                            }
                            dragoverElement = undefined;
                            return;
                        }
                    }

                    if (hasContentBlockSource && !isChild &&
                        targetElement.getAttribute("data-type") === "NodeListItem") {
                        // A plain content block can't become a direct child of a list block.
                        dragoverElement = undefined;
                        return;
                    }

                    if (isChild && targetElement.getAttribute("data-type") === "NodeListItem") {
                        const nestedList = Array.from(targetElement.children).find(c => c.classList.contains("list"));
                        let nestedTarget: Element;
                        if (nestedList) {
                            const liChildren = Array.from(nestedList.children).filter(c => c.classList.contains("li"));
                            if (isBottom) {
                                nestedTarget = liChildren.length > 0 ? liChildren[liChildren.length - 1] : null;
                            } else {
                                nestedTarget = liChildren.length > 0 ? liChildren[0] : null;
                            }
                        }
                        if (nestedTarget) {
                            // When dragging a nested list item to its parent item's position, nestedTarget may
                            // turn out to be the source item itself, so skip it to avoid dropping onto itself
                            if (!sourceElements.includes(nestedTarget)) {
                                dragSame(protyle, sourceElements, nestedTarget, isBottom, isCopyDrag);
                            }
                        } else {
                            // The target list item has no nested list: locate its last content block and insert
                            // after it -- moveTo will automatically create a nested list wrapping the source
                            // list item, forming the child-item structure
                            const contentBlocks = Array.from(targetElement.children).filter(
                                c => c.hasAttribute("data-node-id") && !c.classList.contains("list"));
                            const lastContentBlock = contentBlocks[contentBlocks.length - 1];
                            if (lastContentBlock) {
                                // The nested list is always created after the last content block
                                dragSame(protyle, sourceElements, lastContentBlock, true, isCopyDrag);
                            } else {
                                dragSame(protyle, sourceElements, targetElement, isBottom, isCopyDrag);
                            }
                        }
                    } else if (targetElement.parentElement.getAttribute("data-type") === "NodeSuperBlock" &&
                        targetElement.parentElement.getAttribute("data-sb-layout") === "col") {
                        if (targetClass.includes("dragover__left") || targetClass.includes("dragover__right")) {
                            // Cmd doesn't work for dragging on Mac
                            dragSame(protyle, sourceElements, targetElement, targetClass.includes("dragover__right"), isCopyDrag);
                        } else {
                            dragSb(protyle, sourceElements, targetElement, isBottom, "row", isCopyDrag);
                        }
                    } else {
                        // Dragging a list item to the edge of a list container must not form a row-layout
                        // superblock (dragging a list block to a list edge can still form one)
                        const isListItemOnlySource = gutterTypes[0] === "nodelistitem";
                        const isListTarget = targetElement.classList.contains("list");
                        if (isListItemOnlySource && isListTarget &&
                            (targetClass.includes("dragover__left") || targetClass.includes("dragover__right"))) {
                            // List item dropped on the left/right edge of a list: no-op
                        } else if (targetClass.includes("dragover__left") || targetClass.includes("dragover__right")) {
                            dragSb(protyle, sourceElements, targetElement, targetClass.includes("dragover__right"), "col", isCopyDrag);
                        } else {
                            dragSame(protyle, sourceElements, targetElement, isBottom, isCopyDrag);
                        }
                    }

                    // https://github.com/siyuan-note/siyuan/issues/10528#issuecomment-2205165824
                    editorElement.querySelectorAll(".protyle-wysiwyg--empty").forEach(item => {
                        item.classList.remove("protyle-wysiwyg--empty");
                    });
                }
                dragoverElement = undefined;
            }
        } else if (event.dataTransfer.getData(Constants.SIYUAN_DROP_FILE)?.split("-").length > 1) {
            // Dragging from the file tree
            const ids = event.dataTransfer.getData(Constants.SIYUAN_DROP_FILE).split(",");
            if (!event.altKey && (!targetElement || (
                !targetElement.classList.contains("av__row") && !targetElement.classList.contains("av__gallery-item") &&
                !targetElement.classList.contains("av__gallery-add")
            ))) {
                if (event.y > protyle.wysiwyg.element.lastElementChild.getBoundingClientRect().bottom) {
                    insertEmptyBlock(protyle, "afterend", protyle.wysiwyg.element.lastElementChild.getAttribute("data-node-id"));
                } else {
                    const range = getRangeByPoint(event.clientX, event.clientY);
                    if (hasClosestByAttribute(range.startContainer, "data-type", "NodeBlockQueryEmbed")) {
                        return;
                    } else {
                        focusByRange(range);
                    }
                }
                let html = "";
                for (let i = 0; i < ids.length; i++) {
                    if (ids.length > 1) {
                        html += "- ";
                    }
                    const response = await fetchSyncPost("/api/block/getRefText", {id: ids[i]});
                    html += `((${ids[i]} '${response.data}'))`;
                    if (ids.length > 1 && i !== ids.length - 1) {
                        html += "\n";
                    }
                }
                insertHTML(protyle.lute.Md2BlockDOM(html), protyle);
            } else if (targetElement && !protyle.options.backlinkData && targetElement.className.indexOf("dragover__") > -1) {
                const scrollTop = protyle.contentElement.scrollTop;
                if (targetElement.classList.contains("av__row") ||
                    targetElement.classList.contains("av__gallery-item") ||
                    targetElement.classList.contains("av__gallery-add")) {
                    // Dragging into an attribute view
                    const blockElement = hasClosestBlock(targetElement);
                    if (blockElement) {
                        let previousID = "";
                        if (targetElement.classList.contains("dragover__bottom") || targetElement.classList.contains("dragover__right")) {
                            previousID = targetElement.getAttribute("data-id") || "";
                        } else if (targetElement.classList.contains("dragover__top") || targetElement.classList.contains("dragover__left")) {
                            previousID = targetElement.previousElementSibling?.getAttribute("data-id") || "";
                        }
                        const avID = blockElement.getAttribute("data-av-id");
                        const newUpdated = dayjs().format("YYYYMMDDHHmmss");
                        const srcs: IOperationSrcs[] = [];
                        const bodyElement = hasClosestByClassName(targetElement, "av__body");
                        const groupID = bodyElement && bodyElement.getAttribute("data-group-id");
                        ids.forEach(id => {
                            srcs.push({
                                itemID: Lute.NewNodeID(),
                                id,
                                isDetached: false,
                            });
                        });
                        transaction(protyle, [{
                            action: "insertAttrViewBlock",
                            avID,
                            previousID,
                            srcs,
                            blockID: blockElement.dataset.nodeId,
                            groupID
                        }, {
                            action: "doUpdateUpdated",
                            id: blockElement.dataset.nodeId,
                            data: newUpdated,
                        }], [{
                            action: "removeAttrViewBlock",
                            srcIDs: ids,
                            avID,
                        }, {
                            action: "doUpdateUpdated",
                            id: blockElement.dataset.nodeId,
                            data: blockElement.getAttribute("updated")
                        }]);
                        insertAttrViewBlockAnimation({
                            protyle,
                            blockElement,
                            srcIDs: ids,
                            previousId: previousID,
                            groupID
                        });
                        blockElement.setAttribute("updated", newUpdated);
                    }
                } else {
                    if (targetElement.classList.contains("dragover__bottom")) {
                        for (let i = ids.length - 1; i > -1; i--) {
                            if (ids[i]) {
                                await fetchSyncPost("/api/filetree/doc2Heading", {
                                    srcID: ids[i],
                                    after: true,
                                    targetID: targetElement.getAttribute("data-node-id"),
                                });
                            }
                        }
                    } else {
                        for (let i = 0; i < ids.length; i++) {
                            if (ids[i]) {
                                await fetchSyncPost("/api/filetree/doc2Heading", {
                                    srcID: ids[i],
                                    after: false,
                                    targetID: targetElement.getAttribute("data-node-id"),
                                });
                            }
                        }
                    }

                    const getDocParam: IObject = {
                        id: protyle.block.id,
                        size: window.siyuan.config.editor.dynamicLoadBlocks,
                    };
                    if (isEncryptedBox(protyle.notebookId)) {
                        getDocParam.notebook = protyle.notebookId;
                    }
                    fetchPost("/api/filetree/getDoc", getDocParam, getResponse => {
                        onGet({data: getResponse, protyle});
                        /// #if !MOBILE
                        // After converting between document title and heading, the outline needs to be updated
                        updatePanelByEditor({
                            protyle,
                            focus: false,
                            pushBackStack: false,
                            reload: true,
                            resize: false,
                        });
                        /// #endif
                        // After converting between document title and heading, the editor jumps to the beginning
                        // https://github.com/siyuan-note/siyuan/issues/2939
                        setTimeout(() => {
                            protyle.contentElement.scrollTop = scrollTop;
                            protyle.scroll.lastScrollTop = scrollTop - 1;
                        }, Constants.TIMEOUT_LOAD);
                    });
                }
                targetElement.classList.remove("dragover__bottom", "dragover__top", "dragover__left", "dragover__right");
            }
        } else if (!window.siyuan.dragElement && (
            event.dataTransfer.types.includes("Files") || event.dataTransfer.types.includes("text/html")
        )) {
            event.preventDefault();
            // External files dropped into the editor, or selected text dragged within the editor
            // https://github.com/siyuan-note/siyuan/issues/9544
            const avElement = hasClosestByClassName(event.target, "av");
            if (!avElement) {
                focusByRange(getRangeByPoint(event.clientX, event.clientY));
                if (event.dataTransfer.types.includes("Files") && !isBrowser()) {
                    const files: ILocalFiles[] = [];
                    for (let i = 0; i < event.dataTransfer.files.length; i++) {
                        const filePath = webUtils.getPathForFile(event.dataTransfer.files[i]);
                        if (filePath) {
                            files.push({
                                path: filePath,
                                size: event.dataTransfer.files[i].size
                            });
                        } else {
                            paste(protyle, event);
                            break;
                        }
                    }
                    if (files.length > 0) {
                        uploadLocalFiles(files, protyle, !event.altKey);
                    }
                } else {
                    paste(protyle, event);
                }
                clearSelect(["av", "img"], protyle.wysiwyg.element);
            } else {
                const cellElement = hasClosestByClassName(event.target, "av__cell");
                if (cellElement) {
                    if (getTypeByCellElement(cellElement) === "mAsset" && event.dataTransfer.types[0] === "Files") {
                        /// #if !BROWSER
                        const files: ILocalFiles[] = [];
                        for (let i = 0; i < event.dataTransfer.files.length; i++) {
                            files.push({
                                path: webUtils.getPathForFile(event.dataTransfer.files[i]),
                                size: event.dataTransfer.files[i].size
                            });
                        }
                        dragUpload(files, protyle, cellElement);
                        /// #else
                        focusBlock(hasClosestBlock(cellElement) as HTMLElement);
                        uploadFiles(protyle, event.dataTransfer.files, undefined);
                        /// #endif
                    }
                }
            }
        }
        if (window.siyuan.dragElement) {
            window.siyuan.dragElement.style.opacity = "";
            window.siyuan.dragElement = undefined;
        }
        // Clean up all drag indicators unconditionally after drop/cancel
        cleanupDragIndicators(document);
    });
    let dragoverElement: Element;
    let dragCache: { nodeId: string, indent: number, rgb: { r: number, g: number, b: number }, guides: string };
    let disabledPosition: string;
    // Handle the drop indicator and tooltip for a list-item target: set class, CSS variables, showDragTip
    const applyLiTarget = (htmlTarget: HTMLElement, event: DragEvent, canDropAsSibling = true): void => {
        cleanupDragIndicators(editorElement);
        const nodeId = htmlTarget.getAttribute("data-node-id");
        // Cache expensive computations per target element (never changes while hovering same element)
        if (!dragCache || dragCache.nodeId !== nodeId) {
            const contentBlock = Array.from(htmlTarget.children).find(c => c.hasAttribute("data-node-id")) as HTMLElement;
            const indent = contentBlock ? parseFloat(getComputedStyle(contentBlock).marginLeft) || 34 : 34;
            const depth = getListDepth(htmlTarget);
            const computedColor = getComputedStyle(htmlTarget).getPropertyValue("--b3-theme-primary-lighter").trim();
            const rgb = parseHexColor(computedColor) || {r: 53, g: 115, b: 217};
            let siblingGuides = "";
            for (let n = 1; n <= depth; n++) {
                if (siblingGuides) siblingGuides += ", ";
                // Guide-line opacity fades from 0.5 (nearest) to 0.1 (farthest), both below the insertion
                // line's opacity (0.6) so the target position stands out
                const opacity = depth <= 1 ? 0.3 : 0.5 - (n - 1) / (depth - 1) * 0.4;
                siblingGuides += `${-n * indent}px 0 0 0 rgba(${rgb.r}, ${rgb.g}, ${rgb.b}, ${opacity.toFixed(2)})`;
            }
            dragCache = {nodeId, indent, rgb, guides: siblingGuides || "none"};
        }
        const {indent, rgb, guides} = dragCache;

        const liRect = htmlTarget.getBoundingClientRect();
        const isRTL = getComputedStyle(htmlTarget).direction === "rtl";
        const offsetX = isRTL ? (liRect.right - event.clientX) : (event.clientX - liRect.left);
        // Use the content block's rect (excluding the nested list) to decide top/bottom half, so the bottom
        // zone isn't too small to hit when a nested list is present
        const contentBlockForRect = Array.from(htmlTarget.children).find(c =>
            c.hasAttribute("data-node-id") && !c.classList.contains("list")) as HTMLElement;
        const contentRect = contentBlockForRect ? contentBlockForRect.getBoundingClientRect() : liRect;
        const isBottom = event.clientY > contentRect.top + contentRect.height / 2;
        // The top half of the first list item keeps the top insertion point; every other list item uses a
        // bottom insertion point across its whole area, so the bottom zone isn't too small to hit
        const isFirstLi = !htmlTarget.previousElementSibling || !htmlTarget.previousElementSibling.classList.contains("li");
        let position = "bottom";
        if (isFirstLi && !isBottom) {
            position = "top";
        }
        // When a nested list is present, the mouse can never reach the nested list's area (elementFromPoint hits
        // the child item's .li instead), so the whole content area of a list item with a nested list is treated
        // as sibling (insert as a sibling after the target); without a nested list, offsetX decides child/sibling
        const hasChildList = !!Array.from(htmlTarget.children).find(c => c.classList.contains("list"));
        const isChild = position === "bottom" && !hasChildList && offsetX >= indent;
        if (!canDropAsSibling && !isChild) {
            hideDragTip();
            return;
        }
        // Don't show highlight/tooltip when the source list item is dropped on itself, its descendants, or its
        // original position
        const sourceElements = Array.from(editorElement.querySelectorAll(".protyle-wysiwyg--select")) as HTMLElement[];
        const isNoOp = sourceElements.some(source =>
            source === htmlTarget ||                                    // dropped on itself
            source.contains(htmlTarget) ||                              // dropped on a descendant
            (!isChild && position === "bottom" && source === htmlTarget.nextElementSibling) ||  // sibling below: source is already right after the target
            (position === "top" && source === htmlTarget.previousElementSibling));              // sibling above: source is already right before the target
        if (isNoOp) {
            cleanupDragIndicators(editorElement);
            hideDragTip();
            return;
        }
        const className = `dragover__${position}--${isChild ? "child" : "sibling"}`;

        htmlTarget.classList.add(className);
        htmlTarget.style.setProperty("--drag-indent", `${indent}px`);
        htmlTarget.style.setProperty("--drag-line-left", isChild ? `${indent}px` : "0");
        // The guide line is shown in both sibling and child states (in the sibling state ::before is
        // transparent, so it won't overlap the guide line)
        htmlTarget.style.setProperty("--drag-guides", guides);
        // The ::before target marker only shows when becoming a child item; in the sibling state the horizontal
        // line has exclusive use of that area, to avoid a darker overlap from stacked translucency
        htmlTarget.style.setProperty("--drag-base-bg",
            isChild ? `rgba(${rgb.r}, ${rgb.g}, ${rgb.b}, 0.6)` : "transparent");
        // The horizontal insertion line uses its own dedicated color and is always shown
        htmlTarget.style.setProperty("--drag-line-bg",
            `rgba(${rgb.r}, ${rgb.g}, ${rgb.b}, 0.6)`);
        highlightByLevel(editorElement, htmlTarget);
        // Tooltip text: a modifier key shows the corresponding action; no modifier key shows the insertion position
        const targetText = (getContenteditableElement(htmlTarget)?.textContent?.trim() || "").slice(0, 20);
        let action: string;
        if (event.altKey || (event.shiftKey && protyle.lite)) {
            // Alt = reference; in lite mode, Shift is also a reference
            action = window.siyuan.languages.dragTipRef;
        } else if (event.shiftKey) {
            action = window.siyuan.languages.dragTipEmbed;
        } else if (event.ctrlKey || protyle.lite) {
            // Ctrl = create a copy; in lite mode, no modifier key also means copy
            action = window.siyuan.languages.duplicate;
        } else if (isChild) {
            action = window.siyuan.languages.dragTipListItemChild.replace("${x}", targetText);
        } else {
            const key = position === "bottom" ? "dragTipListItemAfter" : "dragTipListItemBefore";
            action = window.siyuan.languages[key].replace("${x}", targetText);
        }
        showDragTip(window.siyuan.dragTitle || "", action, event.clientX, event.clientY);
    };
    // Cache the current target's text and col-layout check, so the fast path doesn't recompute them on every dragover
    let cachedTargetText = "";
    let cachedIsCol = false;
    editorElement.addEventListener("dragover", (event: DragEvent & { target: HTMLElement }) => {
        if (protyle.disabled || event.dataTransfer.types.includes(Constants.SIYUAN_DROP_EDITOR)) {
            event.preventDefault();
            event.stopPropagation();
            event.dataTransfer.dropEffect = "none";
            hideDragTip();
            return;
        }
        if (event.dataTransfer.types.includes(Constants.SIYUAN_DROP_BLOCK_REF)) {
            if (hasClosestByClassName(event.target, "av")) {
                event.preventDefault();
                event.dataTransfer.dropEffect = "none";
                hideDragTip();
                cleanupDragIndicators(editorElement);
                return;
            }
            event.dataTransfer.dropEffect = "copy";
            showDragTip(window.siyuan.dragTitle || "", window.siyuan.languages.dragTipRef,
                event.clientX, event.clientY);
            renderBlockRefDragover(event);
            return;
        }
        let gutterType = "";
        for (const type of event.dataTransfer.types) {
            if (type.startsWith(Constants.SIYUAN_DROP_GUTTER)) {
                gutterType = type;
            }
        }
        if (gutterType.startsWith(`${Constants.SIYUAN_DROP_GUTTER}NodeAttributeView${Constants.ZWSP}ViewTab${Constants.ZWSP}`.toLowerCase())) {
            dragoverTab(event);
            event.preventDefault();
            return;
        }
        // Parse the gutter type array to distinguish plain blocks, AV blocks, and AV subtypes
        const gutterTypes = gutterType ? gutterType.replace(Constants.SIYUAN_DROP_GUTTER, "").split(Constants.ZWSP) : [];
        const isAvSubType = gutterTypes[0] === "nodeattributeviewrowmenu" ||
            gutterTypes[0] === "nodeattributeviewrow" ||
            (gutterTypes[0] === "nodeattributeview" && ["viewtab", "col", "galleryitem"].includes(gutterTypes[1] || ""));
        // Tooltip: top half = name of the operation's target, bottom half = operation text
        const isAvTarget = hasClosestByClassName(event.target, "av__row") ||
            hasClosestByClassName(event.target, "av__row--util") ||
            hasClosestByClassName(event.target, "av__gallery-item") ||
            hasClosestByClassName(event.target, "av__gallery-add");
        if (event.dataTransfer.types.includes(Constants.SIYUAN_DROP_FILE)) {
            // Dragging a document from the document panel into the editor
            showDragTip(window.siyuan.dragTitle || "",
                isAvTarget ? window.siyuan.languages.addToDatabase :
                    (event.altKey ? window.siyuan.languages.dragTip2Heading : window.siyuan.languages.dragTipRef),
                event.clientX, event.clientY);
        } else if (gutterType && !isAvSubType && !(event.altKey && isInEmbedBlock(event.target))) {
            // A plain block (paragraph/heading/list/quote/AV block, etc., excluding AV row/col/view/card)
            // dragged into the editor
            // Inserting a reference via Alt is not supported when dropping onto an embed block, so skip the tooltip
            let action: string;
            if (isAvTarget) {
                // Dropped onto a database view: bind as a record
                action = window.siyuan.languages.addToDatabase;
            } else if (event.altKey || (event.shiftKey && protyle.lite)) {
                // Alt = reference; in lite mode, Shift is also a reference (what used to be an embed block becomes a reference)
                action = window.siyuan.languages.dragTipRef;
            } else if (event.shiftKey) {
                action = window.siyuan.languages.dragTipEmbed;
            } else if (event.ctrlKey || protyle.lite) {
                // Ctrl = create a copy; in lite mode, no modifier key also means copy (the source block isn't moved)
                action = window.siyuan.languages.duplicate;
            } else {
                action = window.siyuan.languages.move;
            }
            showDragTip(window.siyuan.dragTitle || "", action, event.clientX, event.clientY);
        } else {
            hideDragTip();
        }
        let targetElement: HTMLElement | false;
        // Setting event.dataTransfer.dropEffect = "move" here would prevent drop from observing shift/control
        if (event.dataTransfer.types.includes("Files")) {
            targetElement = hasClosestByClassName(event.target, "av__cell");
            if (targetElement && targetElement.getAttribute("data-dtype") === "mAsset" &&
                !targetElement.classList.contains("av__cell--header")) {
                event.preventDefault(); // omitting this prevents drop from firing
                if (dragoverElement && targetElement === dragoverElement) {
                    return;
                }
                const blockElement = hasClosestBlock(targetElement);
                if (blockElement) {
                    clearSelect(["cell", "row"], protyle.wysiwyg.element);
                    targetElement.classList.add("av__cell--select");
                    if (blockElement.getAttribute("data-av-type") !== "gallery") {
                        addDragFill(targetElement);
                    }
                    dragoverElement = targetElement;
                }
            }
            // Calling event.preventDefault() here would leave no caret https://github.com/siyuan-note/siyuan/issues/12857
            return;
        }

        if (!gutterType && !window.siyuan.dragElement) {
            // https://github.com/siyuan-note/siyuan/issues/6436
            event.preventDefault();
            return;
        }
        const fileTreeIds = (event.dataTransfer.types.includes(Constants.SIYUAN_DROP_FILE) && window.siyuan.dragElement) ? window.siyuan.dragElement.innerText : "";
        if (event.altKey && fileTreeIds.indexOf("-") === -1) {
            // Alt = insert a reference (line-level): follows caret-positioning semantics, so clear all drag indicators.
            // Reuse cleanupDragIndicators so it also covers the list-only indicator classes (--sibling/--child) and
            // the --drag-* CSS variables; otherwise the list indicator line would freeze in place once Alt is held
            // (clearing only the generic classes isn't enough to remove the list indicator).
            // Note: the source block's .protyle-wysiwyg--select class is intentionally kept and not removed here --
            // that class is only added once on dragstart and never restored once removed. When the modifier key is
            // released and dragging returns to normal, the no-op guard relies on this class to recognize the source
            // block, otherwise the source item could be "moved" back onto its own original position. Reference
            // semantics don't depend on this class anyway (they use the id from gutterTypes[2]).
            renderBlockRefDragover(event);
            return;
        }
        // Non-Alt path: clear any leftover Alt vertical-line indicator
        hideCaretLine();
        // Dragging text within the editor, dragging an asset file, or dragging a backlink icon into the editor
        // while holding alt/shift must not run event.preventDefault(), otherwise there's no caret; this must be
        // placed after the !window.siyuan.dragElement check
        event.preventDefault();
        targetElement = hasClosestByClassName(event.target, "av__gallery-item") || hasClosestByClassName(event.target, "av__gallery-add") ||
            hasClosestByClassName(event.target, "av__row") || hasClosestByClassName(event.target, "av__row--util") ||
            hasClosestBlock(event.target);
        const directTargetElement = targetElement;
        if (targetElement && ["gallery", "kanban"].includes(targetElement.getAttribute("data-av-type")) && event.target.classList.contains("av__gallery")) {
            // Dragging into an attribute-view gallery, but no item is selected
            return;
        }
        const point = {x: event.clientX, y: event.clientY, className: ""};

        // If a superblock contains two paragraph blocks a and b, moving into the gap between them makes
        // targetElement resolve to the superblock, which needs to be corrected back to a
        if (targetElement && (targetElement.classList.contains("bq") || targetElement.classList.contains("sb") || targetElement.classList.contains("list") || targetElement.classList.contains("li"))) {
            let prevElement = hasClosestBlock(document.elementFromPoint(point.x, point.y - 6));
            while (prevElement && targetElement.contains(prevElement)) {
                if (getNextBlockSibling(prevElement)) {
                    targetElement = prevElement;
                }
                prevElement = prevElement.parentElement;
            }
        }
        if (!targetElement) {
            if (event.clientY > editorElement.lastElementChild.getBoundingClientRect().bottom) {
                // Hit the bottom
                targetElement = editorElement.lastElementChild as HTMLElement;
                point.className = "dragover__bottom";
            } else if (event.clientY < editorElement.firstElementChild.getBoundingClientRect().top) {
                // Hit the top
                targetElement = editorElement.firstElementChild as HTMLElement;
                point.className = "dragover__top";
            } else {
                const contentRect = protyle.contentElement.getBoundingClientRect();
                const editorPosition = {
                    left: contentRect.left + parseInt(editorElement.style.paddingLeft),
                    right: contentRect.left + protyle.contentElement.clientWidth - parseInt(editorElement.style.paddingRight)
                };
                if (event.clientX < editorPosition.left) {
                    // Left side
                    point.x = editorPosition.left;
                    point.className = "dragover__left";
                } else if (event.clientX >= editorPosition.right) {
                    // Right side
                    point.x = editorPosition.right - 6;
                    point.className = "dragover__right";
                }
                targetElement = document.elementFromPoint(point.x, point.y) as HTMLElement;
                // When a gap is hit, probe upward step by step to find the nearest block-level element (solves
                // the case where a gap below a deeply nested list item can't be hit)
                let probeOffset = 6;
                while (targetElement.classList.contains("protyle-wysiwyg") && probeOffset < 100) {
                    targetElement = document.elementFromPoint(point.x, point.y - probeOffset) as HTMLElement;
                    probeOffset += 6;
                }
                // Gap to the right/left of a superblock: probe inward (horizontally) to find the superblock
                let hProbed = false;
                if (targetElement.classList.contains("protyle-wysiwyg")) {
                    const editorRect = editorElement.getBoundingClientRect();
                    const editorCenter = editorRect.left + editorRect.width / 2;
                    let hProbe = 6;
                    while (targetElement.classList.contains("protyle-wysiwyg") && hProbe < 100) {
                        // Probe left from a right-side gap, probe right from a left-side gap
                        const probeX = point.x > editorCenter ? point.x - hProbe : point.x + hProbe;
                        targetElement = document.elementFromPoint(probeX, point.y) as HTMLElement;
                        hProbe += 6;
                    }
                    hProbed = !targetElement.classList.contains("protyle-wysiwyg");
                }
                // For a list-item source, prefer the deepest .li (precise insertion); for other sources
                // (including list blocks), use the top-level block (to support superblocks)
                if (gutterTypes[0] === "nodelistitem") {
                    let closestLiFromPoint: HTMLElement;
                    if (targetElement.classList.contains("li")) {
                        closestLiFromPoint = targetElement;
                    } else if (targetElement.classList.contains("list")) {
                        // Hit the list container: take the last .li, meaning insert after the end of the list
                        const lis = targetElement.querySelectorAll(":scope > .li");
                        closestLiFromPoint = lis.length > 0 ? lis[lis.length - 1] as HTMLElement : targetElement.closest(".li") as HTMLElement;
                    } else {
                        closestLiFromPoint = targetElement.closest(".li") as HTMLElement;
                    }
                    targetElement = closestLiFromPoint || hasTopClosestByAttribute(targetElement, "data-node-id", null) as HTMLElement;
                } else {
                    targetElement = hasTopClosestByAttribute(targetElement, "data-node-id", null) as HTMLElement;
                }
                if (targetElement && targetElement.classList.contains("sb") && targetElement.getAttribute("data-sb-layout") === "col") {
                    // Keep the whole superblock as target when the mouse is at the editor's left/right edge or
                    // found via horizontal probing, otherwise switch to a child block
                    if (point.className !== "dragover__left" && point.className !== "dragover__right" && !hProbed) {
                        const childElement = targetElement.querySelectorAll("[data-node-id]");
                        targetElement = childElement[point.className === "dragover__left" ? 0 : childElement.length - 1] as HTMLElement;
                    }
                }
            }
        } else if (targetElement && targetElement.classList.contains("list")) {
            // Handle list-item and list-block drags uniformly, so hitting a nested list container behaves consistently
            targetElement = hasClosestBlock(document.elementFromPoint(event.clientX, event.clientY - 6));
        }
        if (gutterType && gutterType.startsWith(`${Constants.SIYUAN_DROP_GUTTER}NodeAttributeView${Constants.ZWSP}Col${Constants.ZWSP}`.toLowerCase())) {
            // A column header can only be dragged into the header row of the current av
            targetElement = hasClosestByClassName(event.target, "av__cell");
            if (targetElement) {
                const targetRowElement = hasClosestByClassName(targetElement, "av__row--header");
                const dragRowElement = hasClosestByClassName(window.siyuan.dragElement, "av__row--header");
                if (targetElement === window.siyuan.dragElement || !targetRowElement || !dragRowElement ||
                    (targetRowElement && dragRowElement && targetRowElement !== dragRowElement)
                ) {
                    targetElement = false;
                }
            }
        } else if (targetElement && gutterType && gutterType.startsWith(`${Constants.SIYUAN_DROP_GUTTER}NodeAttributeViewRowMenu${Constants.ZWSP}`.toLowerCase())) {
            if ((!targetElement.classList.contains("av__row") && !targetElement.classList.contains("av__row--util")) ||
                (window.siyuan.dragElement && !window.siyuan.dragElement.contains(targetElement))) {
                // A row can only be dragged within the current av
                targetElement = false;
            } else {
                const bodyElement = hasClosestByClassName(targetElement, "av__body");
                if (bodyElement) {
                    const blockElement = hasClosestBlock(bodyElement) as HTMLElement;
                    const groupID = bodyElement.getAttribute("data-group-id");
                    // Cross-group dragging is not allowed when the grouping field is template, created, or updated
                    // https://github.com/siyuan-note/siyuan/issues/15553
                    const isTCU = ["template", "created", "updated"].includes(bodyElement.getAttribute("data-dtype"));
                    // Sorting can only be dragged across groups
                    const hasSort = blockElement.querySelector('.block__icon[data-type="av-sort"]')?.classList.contains("block__icon--active");
                    gutterTypes[2].split(",").find(item => {
                        const sourceGroupID = item ? item.split("@")[1] : "";
                        if (sourceGroupID !== groupID && isTCU) {
                            targetElement = false;
                            return true;
                        }
                        if (sourceGroupID === groupID && hasSort) {
                            targetElement = false;
                            return true;
                        }
                    });
                }
            }
        } else if (targetElement && gutterType && gutterType.startsWith(`${Constants.SIYUAN_DROP_GUTTER}NodeAttributeView${Constants.ZWSP}GalleryItem${Constants.ZWSP}`.toLowerCase())) {
            const containerElement = hasClosestByClassName(event.target, "av__container");
            if (targetElement.classList.contains("av") || !containerElement ||
                !containerElement.contains(window.siyuan.dragElement) || targetElement === window.siyuan.dragElement) {
                // A gallery item can only be dragged within the current av
                targetElement = false;
            } else {
                const bodyElement = hasClosestByClassName(targetElement, "av__body");
                if (bodyElement) {
                    const blockElement = hasClosestBlock(bodyElement) as HTMLElement;
                    const groupID = bodyElement.getAttribute("data-group-id");
                    // Cross-group dragging is not allowed when the grouping field is template, created, or updated
                    // https://github.com/siyuan-note/siyuan/issues/15553
                    const isTCU = ["template", "created", "updated"].includes(bodyElement.getAttribute("data-dtype"));
                    // Sorting can only be dragged across groups
                    const hasSort = blockElement.querySelector('.block__icon[data-type="av-sort"]')?.classList.contains("block__icon--active");
                    gutterTypes[2].split(",").find(item => {
                        const sourceGroupID = item ? item.split("@")[1] : "";
                        if (sourceGroupID !== groupID && isTCU) {
                            targetElement = false;
                            return true;
                        }
                        if (sourceGroupID === groupID && hasSort) {
                            targetElement = false;
                            return true;
                        }
                    });
                }
            }
        }

        if (!targetElement) {
            editorElement.querySelectorAll(".dragover__bottom, .dragover__top, .dragover, .dragover__left, .dragover__right").forEach((item: HTMLElement) => {
                item.classList.remove("dragover__top", "dragover__bottom", "dragover", "dragover__left", "dragover__right");
            });
            hideDragTip();
            return;
        }
        // Dragging into an embed block is not allowed (neither the embed block itself nor any content inside it
        // can be a drop target)
        // Exception: when the embed block is the document's first/last block and the cursor is outside its
        // top/bottom edge, it can be dropped as "above/below the embed block" (before/afterend insertion)
        if (targetElement.getAttribute("data-type") === "NodeBlockQueryEmbed") {
            if (editorElement.firstElementChild === targetElement &&
                event.clientY < targetElement.getBoundingClientRect().top) {
                point.className = "dragover__top";
            } else if (editorElement.lastElementChild === targetElement &&
                event.clientY > targetElement.getBoundingClientRect().bottom) {
                point.className = "dragover__bottom";
            } else {
                clearDragoverElement(dragoverElement);
                return;
            }
        } else if (isInEmbedBlock(targetElement)) {
            clearDragoverElement(dragoverElement);
            return;
        }
        const isNotAvItem = !targetElement.classList.contains("av__row") &&
            !targetElement.classList.contains("av__row--util") &&
            !targetElement.classList.contains("av__gallery-item") &&
            !targetElement.classList.contains("av__gallery-add");
        // When targetElement is inside a superblock: only the edges of the outermost child blocks (left of the
        // first child / right of the last child) count as a superblock operation
        if (!targetElement.classList.contains("sb")) {
            const ancestorSb = targetElement.closest('[data-type="NodeSuperBlock"]') as HTMLElement;
            if (ancestorSb) {
                const sbChildBlocks = Array.from(ancestorSb.querySelectorAll("[data-node-id]"));
                const firstBlock = sbChildBlocks[0] as HTMLElement;
                const lastBlock = sbChildBlocks[sbChildBlocks.length - 1] as HTMLElement;
                const isFirstBlock = targetElement === firstBlock || firstBlock.contains(targetElement);
                const isLastBlock = targetElement === lastBlock || lastBlock.contains(targetElement);
                const childRect = targetElement.getBoundingClientRect();
                if ((isFirstBlock && event.clientX < childRect.left + 8) ||
                    (isLastBlock && event.clientX > childRect.right - 8)) {
                    targetElement = ancestorSb;
                }
                // When an entire list block (NodeList) is dragged into a col-layout superblock, the list block
                // itself is a column unit. When the hit point lands on a descendant (.li/.p) of some column's
                // .list, targetElement needs to be promoted to that .list, otherwise the left/right edge
                // indicator line would incorrectly land in front of an inner list item (unable to express
                // "insert to the left/right of this column")
                if (gutterTypes[0] === "nodelist" &&
                    ancestorSb.getAttribute("data-sb-layout") === "col" &&
                    targetElement !== ancestorSb) {
                    const colList = targetElement.closest(".list") as HTMLElement;
                    if (colList && ancestorSb === colList.parentElement) {
                        targetElement = colList;
                    }
                }
            }
        }
        const isListSource = gutterTypes[0] === "nodelistitem" || gutterTypes[0] === "nodelist";
        const isContentBlockSource = !!gutterType && !isListSource && !isAvSubType;
        // Only keep the precise target when a content block truly inside a list item is hit directly; a content
        // block resolved via list-item gap correction is still treated as the list item.
        const keepLiContentTarget = targetElement === directTargetElement && isContentBlockSource &&
            targetElement.parentElement?.getAttribute("data-type") === "NodeListItem";
        // Don't resolve to liTarget when a nested list container or a content block inside a list item is hit;
        // let the generic branch handle it.
        let liTarget = targetElement.classList.contains("list") || keepLiContentTarget ? null :
            (targetElement.getAttribute("data-type") === "NodeListItem"
                ? targetElement : targetElement.parentElement?.getAttribute("data-type") === "NodeListItem"
                    ? targetElement.parentElement : null);
        // No-op when a list item/list block is dropped on a block just outside the list, so the source isn't
        // moved out and left as a standalone list (including multi-level nesting)
        if (isListSource && !liTarget) {
            const sourceSelected = editorElement.querySelector(".protyle-wysiwyg--select") as HTMLElement;
            if (sourceSelected && (sourceSelected.classList.contains("li") || sourceSelected.classList.contains("list"))) {
                // No-op when the source list item/list block is inside the target list container
                if (targetElement.classList.contains("list") && targetElement.contains(sourceSelected)) {
                    cleanupDragIndicators(editorElement);
                    hideDragTip();
                    return;
                }
                // Walk upward from the source, checking whether the target is an adjacent sibling of any-level
                // .list or the .li it belongs to
                let current: Element = sourceSelected;
                while (current && current !== editorElement) {
                    if (current.classList.contains("list") || current.classList.contains("li")) {
                        const checkSiblings = (container: Element) => {
                            let prevSibling = container.previousElementSibling;
                            while (prevSibling && prevSibling.classList.contains("protyle-attr")) {
                                prevSibling = prevSibling.previousElementSibling;
                            }
                            let nextSibling = container.nextElementSibling;
                            while (nextSibling && nextSibling.classList.contains("protyle-attr")) {
                                nextSibling = nextSibling.nextElementSibling;
                            }
                            return targetElement === prevSibling || targetElement === nextSibling;
                        };
                        if (checkSiblings(current)) {
                            // When the source list itself is a top-level document block and the target is its
                            // top-level adjacent block, this is a legitimate top-level reorder (moveTo will
                            // create a new valid list wrapper for the new position), so don't block it
                            if (current.parentElement === editorElement) {
                                break;
                            }
                            cleanupDragIndicators(editorElement);
                            hideDragTip();
                            return;
                        }
                    }
                    current = current.parentElement;
                }
            }
        }
        // When dragging a document from the file tree into the editor, dropping is disallowed by default (Alt
        // must be held to insert it as a reference), and it can't be dropped into itself
        if (liTarget && fileTreeIds.indexOf("-") > -1 && isNotAvItem) {
            if (!event.altKey) {
                return;
            } else if (fileTreeIds.split(",").includes(protyle.block.rootID) && event.altKey) {
                return;
            }
        }
        // When a list item/list block is dropped at the top/bottom of a list container, it's a no-op if the
        // source is inside the list or is already the first/last item of the list
        if (isListSource && targetElement.classList.contains("list")) {
            const sourceSelected = editorElement.querySelector(".protyle-wysiwyg--select");
            // No-op when the source is inside the target list container (a nested list item/list block dragged
            // to its parent list)
            if (sourceSelected && targetElement.contains(sourceSelected)) {
                cleanupDragIndicators(editorElement);
                hideDragTip();
                return;
            }
            const lis = targetElement.querySelectorAll(":scope > .li");
            const lastLi = lis[lis.length - 1];
            const firstLi = lis[0];
            const listRect = targetElement.getBoundingClientRect();
            const isListBottom = event.clientY > listRect.top + listRect.height / 2;
            const sourceIds = Array.from(editorElement.querySelectorAll(".protyle-wysiwyg--select"))
                .map((e: HTMLElement) => e.getAttribute("data-node-id"));
            const isNoOpList = (isListBottom && lastLi && sourceIds.includes(lastLi.getAttribute("data-node-id"))) ||
                (!isListBottom && firstLi && sourceIds.includes(firstLi.getAttribute("data-node-id")));
            if (isNoOpList) {
                cleanupDragIndicators(editorElement);
                hideDragTip();
                return;
            }
        }
        // A list-item target must be handled immediately regardless of whether the fast path was hit, otherwise
        // the tooltip and insertion point are missing when dropping onto the list marker (.protyle-action)
        if (liTarget) {
            // Walk up to find the top-level list container, used to determine the whole list's left/right
            // edges (rather than a nested list's)
            let topList: Element = liTarget as HTMLElement;
            while (topList.parentElement?.classList.contains("li") ||
                   topList.parentElement?.classList.contains("list")) {
                topList = topList.parentElement;
                if (topList.classList.contains("list") && !topList.parentElement?.classList.contains("li")) {
                    break;
                }
            }
            const topListRect = topList.getBoundingClientRect();
            const isLeftEdge = event.clientX < topListRect.left + 32;
            const isRightEdge = event.clientX > topListRect.right - 32;
            if (gutterTypes[0] === "nodelistitem") {
                // List-item drag: the right edge doesn't trigger a superblock (clean up and return); the left
                // edge and the middle go through applyLiTarget
                if (isRightEdge) {
                    cleanupDragIndicators(editorElement);
                    return;
                }
                applyLiTarget(liTarget as HTMLElement, event);
                return;
            }
            // Non-list-item source: an edge doesn't go into applyLiTarget; clear liTarget so the subsequent
            // generic branch handles the row-layout superblock
            if (isLeftEdge || isRightEdge) {
                liTarget = null;
            } else {
                applyLiTarget(liTarget as HTMLElement, event, !isContentBlockSource);
                return;
            }
        }
        if (targetElement && dragoverElement && targetElement === dragoverElement) {
            // Performance optimization: skip re-validation when the target is the same element as before
            const nodeRect = targetElement.getBoundingClientRect();
            cleanupDragIndicators(editorElement);
            editorElement.querySelectorAll("[select-start], [select-end]").forEach((item: HTMLElement) => {
                item.removeAttribute("select-start");
                item.removeAttribute("select-end");
            });
            // File-tree drag restriction
            if (fileTreeIds.indexOf("-") > -1 && isNotAvItem) {
                if (!event.altKey) {
                    return;
                } else if (fileTreeIds.split(",").includes(protyle.block.rootID) && event.altKey) {
                    return;
                }
            }
            if (targetElement.getAttribute("data-type") === "NodeAttributeView" && hasClosestByTag(event.target, "TD")) {
                return;
            }
            // Dropping on itself/a descendant as a plain move (no modifier key) is an invalid move: after
            // releasing Ctrl/Shift/Alt it reverts to a "dragging itself" state and shouldn't show a move
            // indicator. Ctrl (copy)/Shift (embed)/Alt (reference) are allowed to drop on the source's own
            // position (create a copy/embed block/reference), so don't block those.
            const isSelfFast = !event.ctrlKey && !event.shiftKey && !event.altKey && gutterTypes[2]?.split(",").some((item: string) =>
                item && hasClosestByAttribute(targetElement as HTMLElement, "data-node-id", item));
            if (isSelfFast && "nodeattributeviewrowmenu" !== gutterTypes[0]) {
                hideDragTip();
                return;
            }
            if (point.className && !liTarget && !targetElement.classList.contains("sb")) {
                // A list-item drag doesn't trigger a row-layout superblock; don't show an insertion indicator at a list edge
                if (!(gutterTypes[0] === "nodelistitem" && targetElement.classList.contains("list") &&
                    (point.className === "dragover__left" || point.className === "dragover__right"))) {
                    targetElement.classList.add(point.className);
                    addDragover(targetElement);
                    // A .list target has no contenteditable element, so use the first list item's text as the tooltip name
                    let displayText = cachedTargetText;
                    if (!displayText && targetElement.classList.contains("list")) {
                        const firstLi = targetElement.querySelector(":scope > .li");
                        displayText = getContenteditableElement(firstLi as HTMLElement)?.textContent?.trim() || "";
                    }
                    // For the default move (no modifier key, non-AV target, plain block source, not the superblock
                    // itself), update the bottom half with the position text including the target's name
                    if (!event.altKey && !event.shiftKey && !event.ctrlKey && gutterType && !isAvSubType && !isAvTarget && !targetElement.classList.contains("sb")) {
                        const isFront = point.className === "dragover__top" || point.className === "dragover__left";
                        const isBack = point.className === "dragover__bottom" || point.className === "dragover__right";
                        if ((isFront || isBack) && displayText) {
                            // left/right always use front/back; top/bottom are decided by the col layout
                            const isHorizontal = point.className === "dragover__left" || point.className === "dragover__right";
                            const key = (isHorizontal || cachedIsCol)
                                ? (isFront ? window.siyuan.languages.dragTipMoveTargetFront : window.siyuan.languages.dragTipMoveTargetBack)
                                : (isFront ? window.siyuan.languages.dragTipMoveTargetAbove : window.siyuan.languages.dragTipMoveTargetBelow);
                            showDragTip(window.siyuan.dragTitle || "", key.replace("${x}", displayText),
                                event.clientX, event.clientY);
                        }
                    }
                }
                return;
            }

            if (targetElement.classList.contains("av__cell")) {
                if (event.clientX < nodeRect.left + nodeRect.width / 2 && event.clientX > nodeRect.left &&
                    !targetElement.classList.contains("av__row") && targetElement.previousElementSibling !== window.siyuan.dragElement) {
                    targetElement.classList.add("dragover__left");
                } else if (event.clientX > nodeRect.right - nodeRect.width / 2 && event.clientX <= nodeRect.right + 1 &&
                    !targetElement.classList.contains("av__row") && targetElement !== window.siyuan.dragElement.previousElementSibling) {
                    if (window.siyuan.dragElement.previousElementSibling.classList.contains("av__colsticky") &&
                        targetElement === window.siyuan.dragElement.previousElementSibling.lastElementChild) {
                        // Dragged onto the last element of a sticky/frozen column
                    } else {
                        targetElement.classList.add("dragover__right");
                    }
                }
                return;
            }
            // gallery & kanban
            if (targetElement.classList.contains("av__gallery-item")) {
                if (hasClosestByClassName(targetElement, "av__kanban-group")) {
                    const midTop = nodeRect.top + nodeRect.height / 2;
                    if (event.clientY < midTop && event.clientY > nodeRect.top - 13) {
                        targetElement.classList.add("dragover__top");
                    } else if (event.clientY > midTop && event.clientY <= nodeRect.bottom + 13) {
                        targetElement.classList.add("dragover__bottom");
                    }
                } else {
                    const midLeft = nodeRect.left + nodeRect.width / 2;
                    if (event.clientX < midLeft && event.clientX > nodeRect.left - 13) {
                        targetElement.classList.add("dragover__left");
                    } else if (event.clientX > midLeft && event.clientX <= nodeRect.right + 13) {
                        targetElement.classList.add("dragover__right");
                    }
                }
                return;
            }
            if (targetElement.classList.contains("av__gallery-add")) {
                if (hasClosestByClassName(targetElement, "av__kanban-group")) {
                    targetElement.classList.add("dragover__top");
                } else {
                    targetElement.classList.add("dragover__left");
                }
                return;
            }

            // The superblock itself: left/right edges show an insertion point for the whole superblock; a
            // non-edge hit falls through to the generic logic (same as for a paragraph)
            if (targetElement.classList.contains("sb")) {
                const sbRect = targetElement.getBoundingClientRect();
                const isSbLeftEdge = point.className === "dragover__left" || event.clientX < sbRect.left + 32;
                const isSbRightEdge = point.className === "dragover__right" || event.clientX > sbRect.right - 32;
                if (isSbLeftEdge || isSbRightEdge) {
                    const edgeClass = isSbLeftEdge ? "dragover__left" : "dragover__right";
                    targetElement.classList.add(edgeClass);
                    addDragover(targetElement);
                    const sbFirstBlock = targetElement.querySelector("[data-node-id]") as HTMLElement;
                    const sbText = getContenteditableElement(sbFirstBlock)?.textContent?.trim() || "";
                    if (!event.altKey && !event.shiftKey && !event.ctrlKey && gutterType && !isAvSubType && !isAvTarget && sbText) {
                        const key = isSbLeftEdge
                            ? window.siyuan.languages.dragTipMoveTargetFront
                            : window.siyuan.languages.dragTipMoveTargetBack;
                        showDragTip(window.siyuan.dragTitle || "", key.replace("${x}", sbText),
                            event.clientX, event.clientY);
                    }
                    return;
                }
                // Non-edge: don't return, fall through to the generic logic
            }

            // Shrink the left-side gap between two lists so it's easier to drag into it https://github.com/siyuan-note/siyuan/issues/15672
            if (event.clientX < nodeRect.left + (targetElement.classList.contains("list") ? 8 : 32) &&
                event.clientX >= nodeRect.left - 1 &&
                !targetElement.classList.contains("av__row")) {
                targetElement.classList.add("dragover__left");
                addDragover(targetElement);
                // For the default move, update the bottom half with the position text including the target's
                // name (skipped for the superblock itself)
                if (!event.altKey && !event.shiftKey && !event.ctrlKey && gutterType && !isAvSubType && !isAvTarget && !targetElement.classList.contains("sb") && cachedTargetText) {
                    showDragTip(window.siyuan.dragTitle || "",
                        window.siyuan.languages.dragTipMoveTargetFront.replace("${x}", cachedTargetText),
                        event.clientX, event.clientY);
                }
            } else if (event.clientX > nodeRect.right - 32 && event.clientX < nodeRect.right &&
                !targetElement.classList.contains("av__row")) {
                targetElement.classList.add("dragover__right");
                addDragover(targetElement);
                // For the default move, update the bottom half with the position text including the target's
                // name (skipped for the superblock itself)
                if (!event.altKey && !event.shiftKey && !event.ctrlKey && gutterType && !isAvSubType && !isAvTarget && !targetElement.classList.contains("sb") && cachedTargetText) {
                    showDragTip(window.siyuan.dragTitle || "",
                        window.siyuan.languages.dragTipMoveTargetBack.replace("${x}", cachedTargetText),
                        event.clientX, event.clientY);
                }
            } else if (targetElement.classList.contains("av__row--header")) {
                targetElement.classList.add("dragover__bottom");
            } else if (targetElement.classList.contains("av__row--util")) {
                targetElement.previousElementSibling.classList.add("dragover__bottom");
            } else {
                if (event.clientY > nodeRect.top + nodeRect.height / 2 && disabledPosition !== "bottom") {
                    targetElement.classList.add("dragover__bottom");
                    addDragover(targetElement);
                    // For the default move, update the bottom half with the position text including the target's
                    // name (skipped for the superblock itself)
                    if (!event.altKey && !event.shiftKey && !event.ctrlKey && gutterType && !isAvSubType && !isAvTarget && !targetElement.classList.contains("sb") && cachedTargetText) {
                        showDragTip(window.siyuan.dragTitle || "",
                            (cachedIsCol ? window.siyuan.languages.dragTipMoveTargetBack : window.siyuan.languages.dragTipMoveTargetBelow).replace("${x}", cachedTargetText),
                            event.clientX, event.clientY);
                    }
                } else if (disabledPosition !== "top") {
                    targetElement.classList.add("dragover__top");
                    addDragover(targetElement);
                    // For the default move, update the bottom half with the position text including the target's
                    // name (skipped for the superblock itself)
                    if (!event.altKey && !event.shiftKey && !event.ctrlKey && gutterType && !isAvSubType && !isAvTarget && !targetElement.classList.contains("sb") && cachedTargetText) {
                        showDragTip(window.siyuan.dragTitle || "",
                            (cachedIsCol ? window.siyuan.languages.dragTipMoveTargetFront : window.siyuan.languages.dragTipMoveTargetAbove).replace("${x}", cachedTargetText),
                            event.clientX, event.clientY);
                    }
                }
            }
            return;
        }

        if (fileTreeIds.indexOf("-") > -1) {
            if (fileTreeIds.split(",").includes(protyle.block.rootID) && isNotAvItem && event.altKey) {
                dragoverElement = undefined;
                cleanupDragIndicators(editorElement);
                editorElement.querySelectorAll("[select-start], [select-end]").forEach((item: HTMLElement) => {
                    item.removeAttribute("select-start");
                    item.removeAttribute("select-end");
                });
            } else {
                dragoverElement = targetElement;
            }
            return;
        }

        if (gutterType) {
            disabledPosition = "";
            // In-document gutter drag restrictions
            // Exclude itself and its descendants
            if (gutterTypes[0] === "nodeattributeview" && gutterTypes[1] === "col" && targetElement.getAttribute("data-id") === gutterTypes[2]) {
                // A column header can't be dropped onto itself
                clearDragoverElement(dragoverElement);
                return;
            }
            if (gutterTypes[0] === "nodeattributeviewrowmenu" && gutterTypes[2].split("@")[0] === targetElement.getAttribute("data-id")) {
                // A row can't be dropped onto itself
                clearDragoverElement(dragoverElement);
                return;
            }
            const isSelf = gutterTypes[2].split(",").find((item: string) => {
                if (item && hasClosestByAttribute(targetElement as HTMLElement, "data-node-id", item)) {
                    return true;
                }
            });
            if (isSelf && "nodeattributeviewrowmenu" !== gutterTypes[0] && !event.ctrlKey && !event.shiftKey && !event.altKey) {
                // No-op for a plain move onto itself/a descendant; Ctrl (copy)/Shift (embed)/Alt (reference) are
                // allowed to drop on the source's own position (create a copy/embed block/reference), so don't block those
                clearDragoverElement(dragoverElement);
                return;
            }
            if (gutterTypes[0] === "nodelistitem" && "NodeListItem" === targetElement.getAttribute("data-type")) {
                // A non-list selection can't be dragged into a list https://github.com/siyuan-note/siyuan/issues/13822
                const notLiItem = Array.from(protyle.wysiwyg.element.querySelectorAll(".protyle-wysiwyg--select")).find((item: HTMLElement) => {
                    if (!item.classList.contains("li")) {
                        return true;
                    }
                });
                if (notLiItem) {
                    clearDragoverElement(dragoverElement);
                    return;
                }
            }
            if (!["nodelistitem", "nodelist"].includes(gutterTypes[0]) && targetElement.getAttribute("data-type") === "NodeListItem") {
                // A non-list-item can't be dragged around a list item
                clearDragoverElement(dragoverElement);
                return;
            }
            if (gutterTypes[0] === "nodelistitem" && targetElement.parentElement.classList.contains("li") &&
                targetElement.previousElementSibling?.classList.contains("protyle-action")) {
                // A list item can't be dragged above the first element within a list item
                disabledPosition = "top";
            }
            if (gutterTypes[0] === "nodelistitem" &&
                targetElement.nextElementSibling?.classList.contains("list") &&
                // https://github.com/siyuan-note/siyuan/issues/15672
                targetElement.parentElement?.classList.contains("li")) {
                // A list item can't be dragged below the block above a list
                disabledPosition = "bottom";
            }
            if (targetElement && targetElement.classList.contains("av__row--header")) {
                // A block can't be dragged onto the header row
                disabledPosition = "top";
            }
            dragoverElement = targetElement;
            // Update the cache when the target changes
            cachedTargetText = getContenteditableElement(targetElement as HTMLElement)?.textContent?.trim() || "";
            cachedIsCol = !!hasClosestByAttribute(targetElement as HTMLElement, "data-sb-layout", "col");
            highlightColColumn(targetElement as HTMLElement);
        }
        // For the default move (no modifier key, non-AV target, plain block source), update the bottom half
        // with the position text including the target's name
        if (!event.altKey && !event.shiftKey && !event.ctrlKey && gutterType && !isAvSubType && targetElement && !isAvTarget && point.className) {
            const targetText = getContenteditableElement(targetElement as HTMLElement)?.textContent?.trim() || "";
            const isFront = point.className === "dragover__top" || point.className === "dragover__left";
            const isBack = point.className === "dragover__bottom" || point.className === "dragover__right";
            if (targetText && (isFront || isBack)) {
                const isCol = hasClosestByAttribute(targetElement as HTMLElement, "data-sb-layout", "col");
                const key = isCol
                    ? (isFront ? window.siyuan.languages.dragTipMoveTargetFront : window.siyuan.languages.dragTipMoveTargetBack)
                    : (isFront ? window.siyuan.languages.dragTipMoveTargetAbove : window.siyuan.languages.dragTipMoveTargetBelow);
                showDragTip(window.siyuan.dragTitle || "", key.replace("${x}", targetText),
                    event.clientX, event.clientY);
            }
        }
    });
    let counter = 0;
    editorElement.addEventListener("dragleave", (event: DragEvent & { target: HTMLElement }) => {
        if (protyle.disabled) {
            event.preventDefault();
            event.stopPropagation();
            return;
        }
        counter--;
        if (counter === 0) {
            cleanupDragIndicators(editorElement);
            dragoverElement = undefined;
            hideDragTip();
        }
    });
    editorElement.addEventListener("dragenter", (event) => {
        event.preventDefault();
        counter++;
    });
    editorElement.addEventListener("dragend", () => {
        if (window.siyuan.dragElement) {
            window.siyuan.dragElement.style.opacity = "";
            window.siyuan.dragElement = undefined;
            document.onmousemove = null;
        }
        // Clean up all drag indicators on cancel
        cleanupDragIndicators(editorElement);
        dragoverElement = undefined;
        hideDragTip();
        window.siyuan.dragTitle = "";
    });
    // Fallback: document-level cleanup in case dragend doesn't bubble
    document.addEventListener("dragend", () => {
        cleanupDragIndicators(document);
    }, {once: true});
};

const cleanupDragIndicators = (scope: ParentNode) => {
    scope.querySelectorAll(".dragover__top, .dragover__bottom, .dragover__left, .dragover__right, .dragover__top--sibling, .dragover__bottom--sibling, .dragover__top--child, .dragover__bottom--child, .dragover, [style*=\"--drag-indent\"]").forEach((item: HTMLElement) => {
        item.classList.remove("dragover__top", "dragover__bottom", "dragover__left", "dragover__right", "dragover",
            "dragover__top--sibling", "dragover__bottom--sibling", "dragover__top--child", "dragover__bottom--child");
        item.style.removeProperty("--drag-indent");
        item.style.removeProperty("--drag-guides");
        item.style.removeProperty("--drag-line-left");
        item.style.removeProperty("--drag-base-bg");
        item.style.removeProperty("--drag-line-bg");
    });
};

const getListDepth = (liElement: Element): number => {
    let depth = 0;
    let list = liElement.parentElement;
    while (list && list.classList.contains("list")) {
        const parentLi = list.parentElement;
        if (parentLi && parentLi.classList.contains("li")) {
            depth++;
            list = parentLi.parentElement;
        } else {
            break;
        }
    }
    return depth;
};

const parseHexColor = (color: string): { r: number, g: number, b: number } | null => {
    if (!color) return null;
    const hexMatch = color.match(/^#([0-9a-f]{3,8})$/i);
    if (hexMatch) {
        let hex = hexMatch[1];
        if (hex.length === 3) {
            hex = hex[0] + hex[0] + hex[1] + hex[1] + hex[2] + hex[2];
        }
        if (hex.length >= 6) {
            return {
                r: parseInt(hex.slice(0, 2), 16),
                g: parseInt(hex.slice(2, 4), 16),
                b: parseInt(hex.slice(4, 6), 16),
            };
        }
    }
    const rgbMatch = color.match(/rgba?\((\d+),\s*(\d+),\s*(\d+)/);
    if (rgbMatch) {
        return {
            r: parseInt(rgbMatch[1]),
            g: parseInt(rgbMatch[2]),
            b: parseInt(rgbMatch[3]),
        };
    }
    return null;
};

const highlightByLevel = (editorElement: HTMLElement, liElement: HTMLElement) => {
    editorElement.querySelectorAll(".dragover").forEach((item: HTMLElement) => {
        item.classList.remove("dragover");
    });
    liElement.classList.add("dragover");
};

const addDragover = (element: HTMLElement) => {
    if (element.classList.contains("sb") ||
        element.classList.contains("li") ||
        element.classList.contains("list") ||
        element.classList.contains("bq")) {
        element.classList.add("dragover");
    }
    highlightColColumn(element);
};

const highlightColColumn = (element: HTMLElement) => {
    // In a col layout, highlight the column it's in (a column-level sb), to make left/right columns easier to tell apart
    // Only highlight when the target itself is the col superblock; a child-block operation doesn't highlight the whole superblock
    if (element.getAttribute("data-sb-layout") === "col") {
        element.classList.add("dragover");
    }
};

// https://github.com/siyuan-note/siyuan/issues/12651
const clearDragoverElement = (element: Element) => {
    if (element) {
        element.classList.remove("dragover__top", "dragover__bottom", "dragover", "dragover__left", "dragover__right", "dragover__top--sibling", "dragover__bottom--sibling", "dragover__top--child", "dragover__bottom--child");
        (element as HTMLElement).style.removeProperty("--drag-indent");
        (element as HTMLElement).style.removeProperty("--drag-guides");
        (element as HTMLElement).style.removeProperty("--drag-line-left");
        (element as HTMLElement).style.removeProperty("--drag-base-bg");
        element = undefined;
    }
    // Hide the tooltip when dragging is restricted (insertion not allowed), so no leftover "move" text remains
    hideDragTip();
};
