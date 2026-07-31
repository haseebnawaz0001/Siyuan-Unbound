import {Menu} from "../../../plugin/Menu";
import {transaction} from "../../wysiwyg/transaction";
import {escapeAttr, escapeHtml} from "../../../util/escape";
import {getColIconByType} from "./col";
import {setPosition} from "../../../util/setPosition";
import {genCellValue} from "./cell";
import * as dayjs from "dayjs";
import {unicode2Emoji} from "../../../emoji";
import {fetchPost, fetchSyncPost} from "../../../util/fetch";
import {getFieldsByData} from "./view";
import {Constants} from "../../../constants";

export const getDefaultOperatorByType = (type: TAVCol) => {
    if (["select", "number", "date", "created", "updated"].includes(type)) {
        return "=";
    }
    if (["checkbox"].includes(type)) {
        return "=";
    }
    if (["rollup", "relation", "mAsset", "text", "mSelect", "url", "block", "email", "phone", "template"].includes(type)) {
        return "Contains";
    }
};

// getEditableFilters returns the leaf/group array that can be directly added/removed/modified.
// After spec 5 the top level is a single root group whose filters are the edit target; for backward
// compatibility with old flat data, the top-level array is returned directly.
export const getEditableFilters = (data: IAV): IAVFilter[] => {
    if (data.view.filters.length === 1 && (data.view.filters[0].filters || data.view.filters[0].combination)) {
        if (!data.view.filters[0].filters) {
            data.view.filters[0].filters = [];
        }
        return data.view.filters[0].filters;
    }
    return data.view.filters;
};

// getRootFilters returns the root node array used for recursive rendering/traversal (same source as getEditableFilters).
const getRootFilters = (data: IAV): IAVFilter[] => getEditableFilters(data);

// getFilterByPath locates a node in the node tree by an index path (e.g. "0,1,2"). Returns undefined if not found.
export const getFilterByPath = (nodes: IAVFilter[], path: string): IAVFilter => {
    if (!path || "" === path) {
        // An empty/omitted path means the root group itself; but nodes is the root group's children array,
        // so the root group node itself is not in it.
        // Callers that need to operate on the root group node should access data.view.filters[0] directly.
        return undefined;
    }
    const indices = path.split(",").map(i => parseInt(i, 10));
    let current: IAVFilter;
    let list = nodes;
    for (let i = 0; i < indices.length; i++) {
        const idx = indices[i];
        if (!list || isNaN(idx) || idx < 0 || idx >= list.length) {
            return undefined;
        }
        current = list[idx];
        list = current.filters;
    }
    return current;
};

// getParentByPath returns the parent node array at the last path level, along with the last level's index.
// Used to insert/remove child nodes within a specified parent group. An empty path "" is treated as root.
export const getParentByPath = (nodes: IAVFilter[], path: string): { parent: IAVFilter[], index: number } => {
    if (!path || "" === path) {
        return {parent: nodes, index: -1};
    }
    const indices = path.split(",").map(i => parseInt(i, 10));
    const lastIndex = indices.pop();
    if (isNaN(lastIndex) || lastIndex < 0) {
        return {parent: null, index: -1};
    }
    let list = nodes;
    for (const idx of indices) {
        if (!list || isNaN(idx) || idx < 0 || idx >= list.length) {
            return {parent: null, index: -1};
        }
        list = list[idx].filters || (list[idx].filters = []);
    }
    return {parent: list, index: lastIndex};
};

// removeFilterByPath removes a node by path. Returns whether it succeeded. Empty groups are not
// auto-pruned at the UI layer (the user's structure is preserved).
export const removeFilterByPath = (nodes: IAVFilter[], path: string): boolean => {
    const {parent, index} = getParentByPath(nodes, path);
    if (!parent || index < 0 || index >= parent.length) {
        return false;
    }
    parent.splice(index, 1);
    if (parent.length === 0 && path.includes(",")) {
        const groupPath = path.substring(0, path.lastIndexOf(","));
        removeFilterByPath(nodes, groupPath);
    }
    return true;
};

// removeFiltersByColumn recursively removes leaves referencing the given column and prunes groups
// that become empty. Returns the resulting new array.
export const removeFiltersByColumn = (filters: IAVFilter[], column: string): IAVFilter[] => {
    const ret: IAVFilter[] = [];
    filters.forEach(f => {
        if (f.filters) {
            const children = removeFiltersByColumn(f.filters, column);
            if (children.length > 0) {
                ret.push({...f, filters: children});
            }
        } else if (f.column !== column) {
            ret.push(f);
        }
    });
    return ret;
};

// hasFilterForColumn recursively checks whether the filter tree contains a leaf referencing the given column.
export const hasFilterForColumn = (filters: IAVFilter[], column: string): boolean => {
    for (const f of filters) {
        if (f.filters) {
            if (hasFilterForColumn(f.filters, column)) {
                return true;
            }
        } else if (f.column === column) {
            return true;
        }
    }
    return false;
};

// addFilterGroup appends an empty AND group under the group at the given path.
export const addFilterGroup = (data: IAV, path: string) => {
    let target: IAVFilter[];
    if ("" === path) {
        target = getEditableFilters(data);
    } else {
        const node = getFilterByPath(getRootFilters(data), path);
        if (!node) {
            target = getEditableFilters(data);
        } else {
            target = node.filters || (node.filters = []);
        }
    }
    target.push({combination: "and", filters: []});
};

export const addFilter = (options: {
    data: IAV,
    rect: DOMRect,
    menuElement: HTMLElement,
    tabRect: DOMRect,
    avId: string,
    protyle: IProtyle
    blockElement: Element,
    parentPath?: string
}) => {
    const menu = new Menu(Constants.MENU_AV_ADD_FILTER);
    // Locate the target group: supports appending into a specified group; the same group allows multiple
    // conditions on the same column (e.g. Status=Done OR Status=In Progress)
    let targetGroupFilters: IAVFilter[];
    if (options.parentPath && options.parentPath !== "") {
        const node = getFilterByPath(getRootFilters(options.data), options.parentPath);
        targetGroupFilters = node && node.filters ? node.filters : getEditableFilters(options.data);
    } else {
        targetGroupFilters = getEditableFilters(options.data);
    }
    getFieldsByData(options.data).forEach((column) => {
        // Line number type columns cannot be filtered
        if (column.type !== "lineNumber") {
            menu.addItem({
                label: column.name,
                iconHTML: column.icon ? unicode2Emoji(column.icon, "b3-menu__icon", true) : `<svg class="b3-menu__icon"><use xlink:href="#${getColIconByType(column.type)}"></use></svg>`,
                click: () => {
                    const {operator, value} = genEmptyFilterValue(column);
                    const filter: IAVFilter = {
                        column: column.id,
                        operator,
                        value,
                    };
                    // Insert into the target group (reuse targetGroupFilters located during dedup lookup; it holds a
                    // stable reference to that group's child array)
                    const oldFilters = JSON.parse(JSON.stringify(options.data.view.filters));
                    targetGroupFilters.push(filter);
                    const blockID = options.blockElement.getAttribute("data-node-id");
                    // Save the newly added placeholder condition; the inline control is immediately editable (no popup needed)
                    transaction(options.protyle, [{
                        action: "setAttrViewFilters",
                        avID: options.avId,
                        data: JSON.parse(JSON.stringify(options.data.view.filters)),
                        blockID
                    }], [{
                        action: "setAttrViewFilters",
                        avID: options.avId,
                        data: oldFilters,
                        blockID
                    }]);
                    options.menuElement.innerHTML = getFiltersHTML(options.data);
                    setPosition(options.menuElement, options.tabRect.right - options.menuElement.clientWidth, options.tabRect.bottom, options.tabRect.height, 0, true);
                }
            });
        }
    });
    menu.open({
        x: options.rect.left,
        y: options.rect.bottom,
        h: options.rect.height,
    });
};

export const getFiltersHTML = (data: IAV) => {
    let html = "";
    const fields = getFieldsByData(data);
    const measureEl = document.createElement("span");
    measureEl.style.cssText = "position:absolute;visibility:hidden;font-size:14px;white-space:nowrap;";
    document.body.appendChild(measureEl);
    let andOrTextWidth = 0;
    [window.siyuan.languages.filterWhen, window.siyuan.languages.filterCombinationAnd, window.siyuan.languages.filterCombinationOr].forEach(t => {
        measureEl.textContent = t;
        andOrTextWidth = Math.max(andOrTextWidth, measureEl.offsetWidth);
    });
    document.body.removeChild(measureEl);
    // Width must accommodate the text plus b3-select's left/right padding (8 + 26) plus some margin
    const andOrControlWidth = andOrTextWidth + 36;
    const genAndOrSelect = (groupPath: string, combination: string) =>
        `<select class="b3-select" data-type="toggleCombination" data-path="${groupPath}" style="width:${andOrControlWidth}px;"><option value="and" ${combination === "and" ? "selected" : ""}>${window.siyuan.languages.filterCombinationAnd}</option><option value="or" ${combination === "or" ? "selected" : ""}>${window.siyuan.languages.filterCombinationOr}</option></select>`;

    const genWhenLabel = () =>
        `<span class="av__filter-label ft__on-surface" style="width:${andOrControlWidth}px;">${window.siyuan.languages.filterWhen}</span>`;

    const genAndOrLabel = (combination: string) =>
        `<span class="av__filter-label ft__on-surface" style="width:${andOrControlWidth}px;">${combination === "or" ? window.siyuan.languages.filterCombinationOr : window.siyuan.languages.filterCombinationAnd}</span>`;

    const genNodeHTML = (node: IAVFilter, path: string, depth: number, groupPath: string, groupCombination: string, index: number = 0): string => {
        if (!node) {
            return "";
        }
        if (node.filters) {
            const isRoot = 0 === depth;
            const combination = node.combination === "or" ? "or" : "and";
            let childrenHTML = "";
            node.filters.forEach((child, index) => {
                const childPath = path ? `${path},${index}` : `${index}`;
                childrenHTML += genNodeHTML(child, childPath, depth + 1, path, combination, index);
            });

            if (isRoot) {
                return childrenHTML;
            }

            const depthClass = `av__filter-group-children--depth${Math.min(depth, 3)}`;
            const addConditionBtn = depth >= 3
                ? `<span class="block__icon block__icon--text ariaLabel" data-position="4north" data-type="addFilter" data-path="${path}" aria-label="${window.siyuan.languages.addFilterCondition}"><svg><use xlink:href="#iconAdd"></use></svg>${window.siyuan.languages.addFilterCondition}</span>`
                : `<span class="block__icon block__icon--text ariaLabel" data-position="4north" data-type="addFilterCondition" data-path="${path}" data-depth="${depth}" aria-label="${window.siyuan.languages.addFilterCondition}"><svg><use xlink:href="#iconAdd"></use></svg>${window.siyuan.languages.addFilterCondition}<svg><use xlink:href="#iconDown"></use></svg></span>`;

            const andOrHTML = 0 === index ? genWhenLabel() : 1 === index ? genAndOrSelect(groupPath, groupCombination) : genAndOrLabel(groupCombination);
            return `<div class="av__filter-group-item" data-path="${path}">
    <span class="av__filter-group-left">
        ${andOrHTML}
    </span>
    <div class="av__filter-group-children ${depthClass}" data-children="${path}">
        ${childrenHTML}
        <div class="av__filter-group-actions">${addConditionBtn}</div>
    </div>
    <svg class="b3-menu__action ariaLabel" data-position="4west" data-type="moreFilter" data-path="${path}" aria-label="${window.siyuan.languages.more}"><use xlink:href="#iconMore"></use></svg>
</div>`;
        }

        let colData: IAVColumn;
        fields.find((column: IAVColumn) => {
            if (column.id === node.column) {
                colData = column;
                return true;
            }
        });
        if (!colData) {
            return "";
        }
        const iconHTML = colData.icon
            ? unicode2Emoji(colData.icon, "b3-menu__icon", true)
            : `<svg class="b3-menu__icon"><use xlink:href="#${getColIconByType(colData.type)}"></use></svg>`;
        const fieldOptions = fields.filter((f: IAVColumn) => f.type !== "lineNumber").map((f: IAVColumn) =>
            `<option value="${f.id}" ${f.id === node.column ? "selected" : ""}>${escapeHtml(f.name)}</option>`
        ).join("");
        const fieldSelect = `<select class="b3-select fn__flex-1 av__filter-field" data-type="fieldSelect" data-path="${path}">${fieldOptions}</select>`;
        const fieldWrapper = `<span class="av__field-wrapper ariaLabel" data-position="4west" aria-label="${escapeAttr(colData.name)}">${iconHTML}${fieldSelect}</span>`;
        const inlineHTML = genInlineFilterHTML(node, colData, path);
        const leafAndOrHTML = 0 === index ? genWhenLabel() : 1 === index ? genAndOrSelect(groupPath, groupCombination) : genAndOrLabel(groupCombination);
        return `<div class="b3-menu__item av__filter-row" data-path="${path}" data-column="${node.column}">${leafAndOrHTML}<div class="fn__flex-1 av__filter-rowinner">${fieldWrapper}${inlineHTML}</div><svg class="b3-menu__action ariaLabel" data-position="4west" data-type="moreFilter" data-path="${path}" aria-label="${window.siyuan.languages.more}"><use xlink:href="#iconMore"></use></svg></div>`;
    };

    const isRootGroup = data.view.filters.length === 1 && (data.view.filters[0].filters || data.view.filters[0].combination);
    const root = isRootGroup ? data.view.filters[0] : {filters: data.view.filters} as IAVFilter;
    const rootCombination = isRootGroup
        ? (data.view.filters[0].combination === "or" ? "or" : "and")
        : "and";
    html = genNodeHTML(root, "", 0, "", rootCombination);

    const countLeaves = (nodes: IAVFilter[]): number => nodes.reduce((sum, n) => sum + (n.filters ? countLeaves(n.filters) : 1), 0);
    const leafCount = countLeaves(root.filters || []);

    return `<div class="b3-menu__items">
<button class="b3-menu__item" data-type="nobg">
    <span class="block__icon" style="padding: 8px;margin-left: -4px;" data-type="go-config">
        <svg><use xlink:href="#iconLeft"></use></svg>
    </span>
    <span class="b3-menu__label ft__center">${window.siyuan.languages.filter}</span>
</button>
<button class="b3-menu__separator"></button>
${html}
<button class="b3-menu__item" data-type="addFilterCondition" data-path="" data-depth="0">
    <svg class="b3-menu__icon"><use xlink:href="#iconAdd"></use></svg>
    <span class="b3-menu__label av__filter-add-label">${window.siyuan.languages.addFilterCondition}</span>
    <svg class="av__filter-arrow"><use xlink:href="#iconDown"></use></svg>
</button>
<button class="b3-menu__item b3-menu__item--warning${leafCount > 0 ? "" : " fn__none"}" data-type="removeFilters">
    <svg class="b3-menu__icon"><use xlink:href="#iconTrashcan"></use></svg>
    <span class="b3-menu__label">${window.siyuan.languages.removeFilters}</span>
</button>
</div>`;
};

export const duplicateFilterByPath = (nodes: IAVFilter[], path: string): boolean => {
    const {parent, index} = getParentByPath(nodes, path);
    if (!parent || index < 0 || index >= parent.length) {
        return false;
    }
    const clone = JSON.parse(JSON.stringify(parent[index]));
    parent.splice(index + 1, 0, clone);
    return true;
};

export const convertFilterToGroup = (nodes: IAVFilter[], path: string): boolean => {
    const {parent, index} = getParentByPath(nodes, path);
    if (!parent || index < 0 || index >= parent.length) {
        return false;
    }
    const node = parent[index];
    if (node.filters) {
        return false;
    }
    const group: IAVFilter = {
        combination: "and",
        filters: [node],
    };
    parent.splice(index, 1, group);
    return true;
};

export const convertGroupToFilter = (nodes: IAVFilter[], path: string): boolean => {
    const {parent, index} = getParentByPath(nodes, path);
    if (!parent || index < 0 || index >= parent.length) {
        return false;
    }
    const node = parent[index];
    if (!node.filters || 1 !== node.filters.length) {
        return false;
    }
    parent.splice(index, 1, node.filters[0]);
    return true;
};

// ============ Inline filter editing (replaces the setFilter popup) ============

// getOperatorSelectByType generates the operator <select> option HTML based on the value type,
// marking the current operator as selected.
const getOperatorSelectByType = (type: TAVCol, currentOperator: string): string => {
    const opt = (value: string, label: string) => `<option ${value === currentOperator ? "selected" : ""} value="${value}">${label}</option>`;
    switch (type) {
        case "checkbox":
            return opt("=", window.siyuan.languages.filterOperatorIs) + opt("!=", window.siyuan.languages.filterOperatorIsNot);
        case "block":
        case "mAsset":
        case "text":
        case "url":
        case "phone":
        case "email":
            return opt("=", window.siyuan.languages.filterOperatorIs) + opt("!=", window.siyuan.languages.filterOperatorIsNot) +
                opt("Contains", window.siyuan.languages.filterOperatorContains) + opt("Does not contains", window.siyuan.languages.filterOperatorDoesNotContain) +
                opt("Starts with", window.siyuan.languages.filterOperatorStartsWith) + opt("Ends with", window.siyuan.languages.filterOperatorEndsWith) +
                opt("Is empty", window.siyuan.languages.filterOperatorIsEmpty) + opt("Is not empty", window.siyuan.languages.filterOperatorIsNotEmpty);
        case "template":
            return opt("=", window.siyuan.languages.filterOperatorIs) + opt("!=", window.siyuan.languages.filterOperatorIsNot) +
                opt("Contains", window.siyuan.languages.filterOperatorContains) + opt("Does not contains", window.siyuan.languages.filterOperatorDoesNotContain) +
                opt("Starts with", window.siyuan.languages.filterOperatorStartsWith) + opt("Ends with", window.siyuan.languages.filterOperatorEndsWith) +
                opt("Is empty", window.siyuan.languages.filterOperatorIsEmpty) + opt("Is not empty", window.siyuan.languages.filterOperatorIsNotEmpty) +
                opt(">", "&gt;") + opt("<", "&lt;") + opt(">=", "&GreaterEqual;") + opt("<=", "&le;");
        case "date":
        case "created":
        case "updated":
            return opt("=", window.siyuan.languages.filterOperatorIs) + opt(">", window.siyuan.languages.filterOperatorIsAfter) +
                opt("<", window.siyuan.languages.filterOperatorIsBefore) + opt(">=", window.siyuan.languages.filterOperatorIsOnOrAfter) +
                opt("<=", window.siyuan.languages.filterOperatorIsOnOrBefore) + opt("Is between", window.siyuan.languages.filterOperatorIsBetween) +
                opt("Is empty", window.siyuan.languages.filterOperatorIsEmpty) + opt("Is not empty", window.siyuan.languages.filterOperatorIsNotEmpty);
        case "number":
            return opt("=", "=") + opt("!=", "!=") + opt(">", "&gt;") + opt("<", "&lt;") +
                opt(">=", "&GreaterEqual;") + opt("<=", "&le;") +
                opt("Is empty", window.siyuan.languages.filterOperatorIsEmpty) + opt("Is not empty", window.siyuan.languages.filterOperatorIsNotEmpty);
        case "mSelect":
        case "relation":
            return opt("Contains", window.siyuan.languages.filterOperatorContains) + opt("Does not contains", window.siyuan.languages.filterOperatorDoesNotContain) +
                opt("Is empty", window.siyuan.languages.filterOperatorIsEmpty) + opt("Is not empty", window.siyuan.languages.filterOperatorIsNotEmpty);
        case "select":
            return opt("=", window.siyuan.languages.filterOperatorIs) + opt("!=", window.siyuan.languages.filterOperatorIsNot) +
                opt("Is empty", window.siyuan.languages.filterOperatorIsEmpty) + opt("Is not empty", window.siyuan.languages.filterOperatorIsNotEmpty);
        default:
            return "";
    }
};

const rollupTargetColumns = new WeakMap<IAVColumn, IAVColumn>();

// prepareFilterColumns loads the original field that a rollup field points to, so the filter
// control can render according to the original type.
export const prepareFilterColumns = async (data: IAV) => {
    const fields = getFieldsByData(data);
    const avRequests = new Map<string, Promise<IAVColumn[]>>();
    const tasks = fields.filter((column) => column.type === "rollup" && column.rollup?.relationKeyID && column.rollup?.keyID).map(async (column) => {
        const relationColumn = fields.find((item) => item.id === column.rollup.relationKeyID);
        const targetAVID = relationColumn?.relation?.avID;
        if (!targetAVID) {
            return;
        }
        let request = avRequests.get(targetAVID);
        if (!request) {
            request = fetchSyncPost("/api/av/getAttributeView", {id: targetAVID}).then((response) => {
                return (response.data?.av?.keyValues || []).map((item: { key: IAVColumn }) => item.key);
            }).catch(() => []);
            avRequests.set(targetAVID, request);
        }
        const targetColumns = await request;
        const targetColumn = targetColumns.find((item) => item.id === column.rollup.keyID);
        if (targetColumn) {
            rollupTargetColumns.set(column, targetColumn);
        }
    });
    await Promise.all(tasks);
};

// resolveFilterValueType resolves the actual value type of a filter.
// For rollup types, the calculation result type is used first; otherwise the type of the original
// field the rollup points to is used.
const resolveFilterValueType = (filter: IAVFilter, colData: IAVColumn): { type: TAVCol, colData: IAVColumn, isRollup: boolean } => {
    const valueType = filter.value?.type as TAVCol;
    if (valueType !== "rollup") {
        return {type: valueType, colData, isRollup: false};
    }
    const targetColumn = rollupTargetColumns.get(colData);
    const rollup = filter.value?.rollup;
    const contentType = rollup?.contents?.[0]?.type as TAVCol;
    const calcOperator = colData.rollup?.calc?.operator;
    const numberOperators = [
        "Count all", "Count values", "Count unique values", "Count empty", "Count not empty",
        "Percent empty", "Percent not empty", "Percent unique values", "Sum", "Average", "Median", "Min", "Max",
        "Checked", "Unchecked", "Percent checked", "Percent unchecked",
    ];
    const resolvedType = numberOperators.includes(calcOperator)
        ? "number"
        : targetColumn?.type || contentType || "text";
    return {type: resolvedType, colData: targetColumn || colData, isRollup: true};
};

const getFilterCellValue = (filter: IAVFilter) => filter.value?.type === "rollup"
    ? filter.value.rollup?.contents?.[0]
    : filter.value;

const escapeFilterValue = (value: string) => escapeAttr(escapeHtml(value));

const genEmptyCellValue = (type: TAVCol): IAVCellValue => type === "checkbox"
    ? genCellValue(type, {checked: undefined})
    : {type} as IAVCellValue;

const genEmptyFilterValue = (column: IAVColumn): { operator: TAVFilterOperator, value: IAVCellValue } => {
    if (column.type !== "rollup") {
        return {
            operator: getDefaultOperatorByType(column.type),
            value: genEmptyCellValue(column.type),
        };
    }
    const emptyRollup = {type: "rollup", rollup: {contents: []}} as IAVCellValue;
    const {type} = resolveFilterValueType({value: emptyRollup} as IAVFilter, column);
    return {
        operator: getDefaultOperatorByType(type),
        value: {
            type: "rollup",
            rollup: {contents: [genEmptyCellValue(type)]},
        } as IAVCellValue,
    };
};

// genInlineFilterHTML generates the inline editable HTML for a single leaf filter condition
// (operator select + value control).
// Replaces the old read-only chip from genFilterItem. colData is the column config for this
// column (including options/relation/rollup, etc.).
const genInlineFilterHTML = (filter: IAVFilter, colData: IAVColumn, path: string): string => {
    const {type: valueType, colData: valueColumn, isRollup} = resolveFilterValueType(filter, colData);
    const operator = filter.operator;
    const isEmptyOp = operator === "Is empty" || operator === "Is not empty";
    const valueHidden = isEmptyOp ? " fn__none" : "";

    // Operator select
    const operatorSelect = `<select class="b3-select" data-type="operation" data-path="${path}">${getOperatorSelectByType(valueType, operator)}</select>`;

    // Quantifier select (only present for rollup/mAsset)
    const quantifierSelect = (isRollup || valueType === "mAsset")
        ? `<select class="b3-select" data-type="quantifier" data-path="${path}">
<option ${(!filter.quantifier || filter.quantifier === "Any") ? "selected" : ""} value="Any">${window.siyuan.languages.filterQuantifierAny}</option>
<option ${filter.quantifier === "All" ? "selected" : ""} value="All">${window.siyuan.languages.filterQuantifierAll}</option>
<option ${filter.quantifier === "None" ? "selected" : ""} value="None">${window.siyuan.languages.filterQuantifierNone}</option>
</select>`
        : "";

    // Value control (by type)
    let valueHTML = "";
    let extraHTML = ""; // Extra HTML placed outside valueContainer (e.g. select dropdown panel, to avoid affecting row width)
    const filterValue = getFilterCellValue(filter);
    if (["text", "url", "block", "email", "phone", "template"].includes(valueType)) {
        const content = filterValue?.[valueType as "text"]?.content || "";
        valueHTML = `<input class="b3-text-field b3-text-field--text fn__flex-1" value="${escapeFilterValue(content)}" data-type="filterValue" data-path="${path}">`;
    } else if (valueType === "mAsset") {
        const content = filterValue?.mAsset?.[0]?.content || "";
        valueHTML = `<input class="b3-text-field b3-text-field--text fn__flex-1" value="${escapeFilterValue(content)}" data-type="filterValue" data-path="${path}">`;
    } else if (valueType === "number") {
        const content = filterValue?.number?.isNotEmpty ? filterValue.number.content : "";
        valueHTML = `<input class="b3-text-field b3-text-field--text av__filter-num" value="${content}" data-type="filterValue" data-path="${path}">`;
    } else if (valueType === "checkbox") {
        const isChecked = filterValue?.checkbox?.checked;
        valueHTML = `<select class="b3-select" data-type="filterValue" data-path="${path}"><option value="true" ${isChecked ? "selected" : ""}>${window.siyuan.languages.checked}</option><option value="false" ${!isChecked ? "selected" : ""}>${window.siyuan.languages.unchecked}</option></select>`;
    } else if (["date", "created", "updated"].includes(valueType)) {
        valueHTML = genInlineDateHTML(filter, valueType, path);
    } else if (valueType === "select" || valueType === "mSelect") {
        const {trigger, dropdown} = genInlineSelectHTML(filter, valueColumn, path, valueType);
        valueHTML = trigger;
        extraHTML = dropdown; // Dropdown panel placed outside valueContainer; fixed positioning doesn't affect row width
    } else if (valueType === "relation") {
        const content = filterValue?.relation?.blockIDs?.[0] || "";
        valueHTML = `<input class="b3-text-field b3-text-field--text fn__flex-1" value="${escapeFilterValue(content)}" data-type="filterValue" data-type-rel="relation" data-path="${path}">`;
    }

    return `${quantifierSelect}${operatorSelect}<span class="av__filter-value${valueHidden}" data-type="valueContainer" data-path="${path}">${valueHTML}</span>${extraHTML}`;
};

// genInlineDateHTML generates the inline control for date type columns (absolute/relative toggle + Is between end date).
const genInlineDateHTML = (filter: IAVFilter, valueType: TAVCol, path: string): string => {
    const dateValue = getFilterCellValue(filter)?.[valueType as "date"];
    const showToday1 = !filter.relativeDate?.direction;
    const showToday2 = !filter.relativeDate2?.direction;
    const isBetween = filter.operator === "Is between";

    // formatAbsDate formats a timestamp as yyyy-MM-dd; empty/invalid values return "" to avoid
    // <input type="date"> showing "Invalid Date".
    // 0 is also treated as empty: for created/updated types, content becomes 0 after a null round-trips
    // through the backend's int64 handling; otherwise dayjs(0) would render as 1970-01-01 (this keeps it
    // consistent with the blank appearance of date type when isNotEmpty is false).
    const formatAbsDate = (timestamp: any): string => {
        if (!timestamp) {
            return "";
        }
        const dayObj = dayjs(timestamp);
        return dayObj.isValid() ? dayObj.format("YYYY-MM-DD") : "";
    };

    const dateBlock = (suffix: "" | "2", relativeDate: IAVRelativeDate, dateVal: any, showToday: boolean): string => {
        const dateTypeSel = `<select class="b3-select" data-type="dateType${suffix}" data-path="${path}">
<option value="time"${!relativeDate ? " selected" : ""}>${window.siyuan.languages.includeTime}</option>
<option value="custom"${relativeDate ? " selected" : ""}>${window.siyuan.languages.relativeToToday}</option>
</select>`;
        const absDate = `<input value="${(dateVal && (dateVal.isNotEmpty || (suffix === "2" ? dateVal.isNotEmpty2 : valueType !== "date"))) ? formatAbsDate(suffix === "2" ? dateVal.content2 : dateVal.content) : ""}" type="date" max="9999-12-31" class="b3-text-field b3-text-field--text" data-type="absDate${suffix}" data-path="${path}" style="${relativeDate ? "display:none;" : ""}">`;
        const relDir = `<select class="b3-select" data-type="dataDirection${suffix}" data-path="${path}" style="${!relativeDate ? "display:none;" : ""}">
<option value="-1"${relativeDate?.direction === -1 ? " selected" : ""}>${window.siyuan.languages.pastDate}</option>
<option value="1"${relativeDate?.direction === 1 ? " selected" : ""}>${window.siyuan.languages.nextDate}</option>
<option value="0"${showToday ? " selected" : ""}>${window.siyuan.languages.current}</option>
</select>`;
        // The count is meaningless under the "current" direction (the backend takes today/this week/this
        // month/this year based on the unit), so only relCount is hidden;
        // but the relUnit unit must be kept so the user can choose day/week/month/year
        const relCount = `<input type="number" min="1" step="1" value="${relativeDate?.count || 1}" class="b3-text-field b3-text-field--text av__filter-num" data-type="relCount${suffix}" data-path="${path}" style="${(!relativeDate || showToday) ? "display:none;" : ""}">`;
        const relUnit = `<select class="b3-select" data-type="relUnit${suffix}" data-path="${path}" style="${!relativeDate ? "display:none;" : ""}">
<option value="0"${relativeDate?.unit === 0 ? " selected" : ""}>${window.siyuan.languages.day}</option>
<option value="1"${(!relativeDate || relativeDate?.unit === 1) ? " selected" : ""}>${window.siyuan.languages.week}</option>
<option value="2"${relativeDate?.unit === 2 ? " selected" : ""}>${window.siyuan.languages.month}</option>
<option value="3"${relativeDate?.unit === 3 ? " selected" : ""}>${window.siyuan.languages.year}</option>
</select>`;
        return `<span class="av__filter-date-row">${dateTypeSel}${absDate}${relDir}${relCount}${relUnit}</span>`;
    };

    const filter1 = dateBlock("", filter.relativeDate, dateValue, showToday1);
    const filter2 = dateBlock("2", filter.relativeDate2, dateValue, showToday2);
    return `<span class="av__filter-date-col">${filter1}<span data-type="filter2Wrap" data-path="${path}" style="${isBetween ? "" : "display:none;"}">${filter2}</span></span>`;
};

// genInlineSelectHTML generates the inline multi-select chip list + search for select/mSelect.
const genInlineSelectHTML = (filter: IAVFilter, colData: IAVColumn, path: string, valueType: TAVCol): { trigger: string, dropdown: string } => {
    const isSingle = valueType === "select";
    const options = colData.options || [];
    const selectedValues = (getFilterCellValue(filter)?.mSelect || []).filter((s: IAVCellSelectValue) => s.content);
    const placeholder = isSingle ? window.siyuan.languages.select : window.siyuan.languages.multiSelect;

    // Trigger: shows chips for selected values (consistent with the table cell style); when nothing is
    // selected, shows a placeholder + dropdown arrow
    const selectedChips = selectedValues.map((item: IAVCellSelectValue) => {
        return `<span class="b3-chip b3-chip--middle av__select-chip" style="background-color:var(--b3-font-background${item.color});color:var(--b3-font-color${item.color})">${escapeHtml(item.content)}</span>`;
    }).join("");
    const triggerContent = selectedChips || `<span class="ft__on-surface fn__ellipsis">${placeholder}</span>`;
    const trigger = `<span class="av__select-trigger" data-type="selectTrigger" data-path="${path}">${triggerContent}<svg class="av__select-trigger-arrow"><use xlink:href="#iconDown"></use></svg></span>`;

    // Dropdown panel
    const searchInput = options.length > 5
        ? `<input class="b3-text-field" placeholder="${window.siyuan.languages.search}" data-type="filterSearch" data-path="${path}">`
        : "";
    const chips = options.map((option: { name: string; color: string; desc?: string }) => {
        const selected = selectedValues.some((s: IAVCellSelectValue) => s.content === option.name);
        return `<span class="b3-chip b3-chip--middle${selected ? " b3-chip--primary" : ""} av__select-option" data-name="${escapeAttr(option.name)}" data-color="${option.color}" data-type="selectOption" data-path="${path}" style="background-color:var(--b3-font-background${option.color});color:var(--b3-font-color${option.color})">
<svg class="icon"><use xlink:href="#${selected ? "iconCheck" : "iconUncheck"}"></use></svg>
<span class="fn__ellipsis">${escapeHtml(option.name)}</span>
</span>`;
    }).join("");
    const dropdown = `<div class="av__select-dropdown" data-type="selectDropdown" data-path="${path}" data-single="${isSingle ? "true" : "false"}" style="display:none;">
${searchInput}<div class="av__select-options" data-type="selectOptions" data-path="${path}">${chips}</div>
</div>`;
    return {trigger, dropdown};
};

// readInlineValue reads the value from the leaf row's DOM, returning { value, relativeDate,
// relativeDate2 } based on the type.
// Fix #1: date uses data-type for precise targeting, replacing the old global textElements index.
const readInlineValue = (rowElement: HTMLElement, valueType: TAVCol, operator: string, filter: IAVFilter): { newValue: IAVCellValue, relativeDate: IAVRelativeDate, relativeDate2: IAVRelativeDate } => {
    let newValue: IAVCellValue = filter.value;
    let relativeDate: IAVRelativeDate = filter.relativeDate;
    let relativeDate2: IAVRelativeDate = filter.relativeDate2;

    if (operator === "Is empty" || operator === "Is not empty") {
        // Empty operator: keep the value's type shell with no actual content
        newValue = genEmptyCellValue(valueType);
        relativeDate = undefined;
        relativeDate2 = undefined;
    } else if (valueType === "checkbox") {
        const select = rowElement.querySelector('[data-type="filterValue"]') as HTMLSelectElement;
        const isChecked = select?.value !== "false";
        newValue = genCellValue("checkbox", {checked: isChecked});
    } else if (valueType === "relation") {
        const input = rowElement.querySelector('[data-type="filterValue"]') as HTMLInputElement;
        newValue = input?.value ? genCellValue("relation", input.value) : genEmptyCellValue("relation");
    } else if (["text", "url", "block", "email", "phone", "template", "mAsset", "number"].includes(valueType)) {
        const input = rowElement.querySelector('[data-type="filterValue"]') as HTMLInputElement;
        const val = input?.value || "";
        newValue = val ? genCellValue(valueType, val) : genEmptyCellValue(valueType);
    } else if (["date", "created", "updated"].includes(valueType)) {
        // Fix #1: use data-type to precisely target the absolute date input
        const dateTypeSel = rowElement.querySelector('[data-type="dateType"]') as HTMLSelectElement;
        const isRelative = dateTypeSel?.value === "custom";
        if (isRelative) {
            relativeDate = readRelativeDate(rowElement, "");
            if (operator === "Is between") {
                relativeDate2 = readRelativeDate(rowElement, "2");
            } else {
                relativeDate2 = undefined;
            }
            newValue = {type: valueType} as IAVCellValue;
        } else {
            const absDate1 = rowElement.querySelector('[data-type="absDate"]') as HTMLInputElement;
            const dateStr1 = absDate1?.value || "";
            const content1 = dateStr1 ? new Date(dateStr1 + " 00:00").getTime() : 0;
            const isNotEmpty = !!dateStr1;
            let content2 = 0;
            let isNotEmpty2 = false;
            let dateStr2 = "";
            if (operator === "Is between") {
                const absDate2 = rowElement.querySelector('[data-type="absDate2"]') as HTMLInputElement;
                dateStr2 = absDate2?.value || "";
                content2 = dateStr2 ? new Date(dateStr2 + " 00:00").getTime() : 0;
                isNotEmpty2 = !!dateStr2;
            }
            newValue = {
                type: valueType,
                [valueType]: {
                    content: content1,
                    isNotEmpty,
                    content2,
                    isNotEmpty2,
                    hasEndDate: operator === "Is between" && isNotEmpty2,
                    isNotTime: true,
                },
            } as IAVCellValue;
            relativeDate = undefined;
            relativeDate2 = undefined;
        }
    } else if (valueType === "select" || valueType === "mSelect") {
        // Scan the dropdown panel for selected chips (#iconCheck). The dropdown is outside the row
        // (fixed positioning), so look it up globally by path
        const path = rowElement.dataset.path;
        const mSelect: IAVCellSelectValue[] = [];
        const dropdown = document.querySelector(`[data-type="selectDropdown"][data-path="${path}"]`);
        const searchRoot = dropdown || rowElement; // Fallback: for backward compatibility with the old structure
        searchRoot.querySelectorAll('[data-type="selectOption"]').forEach((chip: HTMLElement) => {
            const useEl = chip.querySelector("use");
            if (useEl && useEl.getAttribute("xlink:href") === "#iconCheck") {
                mSelect.push({content: chip.dataset.name, color: chip.dataset.color});
            }
        });
        newValue = mSelect.length > 0 ? genCellValue(valueType, mSelect) : genEmptyCellValue(valueType);
    }

    // rollup wrapping
    if (filter.value?.type === "rollup") {
        newValue = {type: "rollup", rollup: {contents: [newValue]}} as IAVCellValue;
    }

    return {newValue, relativeDate, relativeDate2};
};

// readRelativeDate reads the relative date config from the row (suffix is "" or "2").
const readRelativeDate = (rowElement: HTMLElement, suffix: string): IAVRelativeDate => {
    const dirSel = rowElement.querySelector(`[data-type="dataDirection${suffix}"]`) as HTMLSelectElement;
    const countInput = rowElement.querySelector(`[data-type="relCount${suffix}"]`) as HTMLInputElement;
    const unitSel = rowElement.querySelector(`[data-type="relUnit${suffix}"]`) as HTMLSelectElement;
    const direction = parseInt(dirSel?.value || "0", 10);
    return {
        count: parseInt(countInput?.value || "1", 10),
        unit: parseInt(unitSel?.value || "0", 10) as 0 | 1 | 2 | 3,
        direction: direction as -1 | 0 | 1,
    };
};

// commitFilter immediately saves changes to a single condition. When reRender=true, re-renders
// the whole panel (for structural change scenarios).
export const commitFilter = (data: IAV, path: string, newFilter: IAVFilter, protyle: IProtyle, blockID: string, avID: string, menuElement: HTMLElement, reRender: boolean) => {
    const editable = getEditableFilters(data);
    const {parent, index} = getParentByPath(editable, path);
    if (!parent || index < 0 || index >= parent.length) {
        return;
    }
    const oldFilters = JSON.parse(JSON.stringify(data.view.filters));
    parent[index] = newFilter;

    transaction(protyle, [{
        action: "setAttrViewFilters",
        avID,
        data: JSON.parse(JSON.stringify(data.view.filters)),
        blockID
    }], [{
        action: "setAttrViewFilters",
        avID,
        data: oldFilters,
        blockID
    }]);

    if (reRender && menuElement) {
        menuElement.innerHTML = getFiltersHTML(data);
    }
};

// bindInlineFilterEvents binds events for inline filter editing (delegated to the panel). Saves immediately.
export const bindInlineFilterEvents = (panelElement: HTMLElement, data: IAV, protyle: IProtyle, blockID: string, avID: string) => {
    // Prevent duplicate binding: events are delegated on panelElement, only needs to be bound once per panel instance
    if (panelElement.dataset.filterEventsBound === "true") {
        return;
    }
    panelElement.dataset.filterEventsBound = "true";
    const menuElement = panelElement.querySelector(".b3-menu") as HTMLElement;
    const fields = getFieldsByData(data);

    // Locate the leaf row via data-path
    const getRow = (target: HTMLElement): HTMLElement => {
        const path = target.dataset.path;
        if (!path) return null;
        return menuElement.querySelector(`[data-path="${path}"]`) as HTMLElement;
    };

    // Find the column config
    const findColData = (path: string): IAVColumn => {
        const filter = getFilterByPath(getEditableFilters(data), path);
        if (!filter) return null;
        let colData: IAVColumn;
        fields.find((column: IAVColumn) => {
            if (column.id === filter.column) {
                colData = column;
                return true;
            }
        });
        return colData;
    };

    // Save the current row's value (read from DOM then submit)
    const saveRow = (rowElement: HTMLElement, path: string, reRender: boolean) => {
        const filter = getFilterByPath(getEditableFilters(data), path);
        const colData = findColData(path);
        if (!filter || !colData) return;
        const {type: valueType} = resolveFilterValueType(filter, colData);
        const operatorSel = rowElement.querySelector('[data-type="operation"]') as HTMLSelectElement;
        const operator = (operatorSel?.value || filter.operator) as TAVFilterOperator;
        const {newValue, relativeDate, relativeDate2} = readInlineValue(rowElement, valueType, operator, filter);
        const quantifierSel = rowElement.querySelector('[data-type="quantifier"]') as HTMLSelectElement;
        const newFilter: IAVFilter = {
            column: filter.column,
            operator,
            value: newValue,
            relativeDate,
            relativeDate2,
        };
        if (quantifierSel) {
            newFilter.quantifier = quantifierSel.value;
        }
        commitFilter(data, path, newFilter, protyle, blockID, avID, menuElement, reRender);
    };

    // operator change: switching the operator may require a re-render (structural changes like Is between/Is empty)
    panelElement.addEventListener("change", (event: Event) => {
        const target = event.target as HTMLElement;
        const type = target.dataset.type;
        if (!type) return;
        const path = target.dataset.path;
        if (!path) return;
        const row = getRow(target);
        if (!row) return;

        if (type === "fieldSelect") {
            // Switching field: replace with the new field's default operator + empty value, re-render the whole thing
            const newColId = (target as HTMLSelectElement).value;
            const newColData = fields.find((f: IAVColumn) => f.id === newColId);
            if (newColData) {
                const {operator, value} = genEmptyFilterValue(newColData);
                const newFilter: IAVFilter = {
                    column: newColId,
                    operator,
                    value,
                };
                commitFilter(data, path, newFilter, protyle, blockID, avID, menuElement, true);
            }
        } else if (type === "operation") {
            // Determine whether this is a structural change (requiring a re-render): toggling date's Is
            // between, or toggling an empty operator
            const filter = getFilterByPath(getEditableFilters(data), path);
            const colData = findColData(path);
            const {type: valueType} = resolveFilterValueType(filter, colData);
            const newOp = (target as HTMLSelectElement).value;
            const oldOp = filter.operator;
            const structureChange = (["date", "created", "updated"].includes(valueType) &&
                ((newOp === "Is between") !== (oldOp === "Is between"))) ||
                ((newOp === "Is empty" || newOp === "Is not empty") !== (oldOp === "Is empty" || oldOp === "Is not empty"));
            saveRow(row, path, structureChange);
        } else if (type === "quantifier" || type?.startsWith("dataDirection") || type?.startsWith("dateType")) {
            // Quantifier, date direction, date type changes: save. Toggling dateType (absolute/relative) or
            // dataDirection (current/past/future) both change the visibility state of relCount/relUnit,
            // requiring a re-render
            if (type === "dateType" || type === "dateType2" || type?.startsWith("dataDirection")) {
                saveRow(row, path, true);
            } else {
                saveRow(row, path, false);
            }
        } else if (type === "relUnit" || type === "relUnit2") {
            saveRow(row, path, false);
        } else if (type === "filterValue") {
            // Triggered by select/change (a change not from keyboard input, e.g. browser autofill)
            saveRow(row, path, false);
        }
    });

    // Save on value input blur / Enter
    panelElement.addEventListener("blur", (event: Event) => {
        const target = event.target as HTMLElement;
        if (target.dataset.type === "filterValue" || target.dataset.type?.startsWith("absDate") || target.dataset.type?.startsWith("relCount")) {
            const path = target.dataset.path;
            const row = getRow(target);
            if (path && row) saveRow(row, path, false);
        }
    }, true); // capture to catch blur (blur doesn't bubble)

    panelElement.addEventListener("keydown", (event: KeyboardEvent) => {
        const target = event.target as HTMLElement;
        if (event.key !== "Enter" || event.isComposing) return;
        if (target.dataset.type === "filterValue") {
            const path = target.dataset.path;
            const row = getRow(target);
            if (path && row) {
                saveRow(row, path, false);
                event.preventDefault();
            }
        }
    });

    // select dropdown trigger: click to expand/collapse the options panel
    panelElement.addEventListener("click", (event: MouseEvent) => {
        const target = event.target as HTMLElement;
        // First handle selectTrigger (expand/collapse the dropdown)
        const trigger = target.closest('[data-type="selectTrigger"]') as HTMLElement;
        if (trigger) {
            const path = trigger.dataset.path;
            // The dropdown panel has been moved outside the row (fixed positioning); look it up globally by path
            const dropdown = menuElement.querySelector(`[data-type="selectDropdown"][data-path="${path}"]`) as HTMLElement;
            if (dropdown) {
                // Collapse any other expanded dropdowns
                menuElement.querySelectorAll('[data-type="selectDropdown"]').forEach((el: HTMLElement) => {
                    if (el !== dropdown) el.style.display = "none";
                });
                if (dropdown.style.display === "none") {
                    // When expanding, use fixed positioning below the trigger (to avoid being clipped by overflow:auto)
                    const rect = trigger.getBoundingClientRect();
                    dropdown.style.zIndex = (++window.siyuan.zIndex).toString();
                    dropdown.style.left = rect.left + "px";
                    dropdown.style.width = Math.max(rect.width, 120) + "px";
                    // Temporarily show it first to measure the real height, then decide whether to expand up or down
                    dropdown.style.visibility = "hidden";
                    dropdown.style.display = "block";
                    const dropdownHeight = dropdown.offsetHeight;
                    dropdown.style.visibility = "";
                    const spaceBelow = window.innerHeight - rect.bottom;
                    if (spaceBelow < dropdownHeight + 8 && rect.top > dropdownHeight + 8) {
                        // Not enough space below but enough above: expand upward, flush against the top of the trigger
                        dropdown.style.top = (rect.top - dropdownHeight - 4) + "px";
                    } else {
                        // Expand downward
                        dropdown.style.top = (rect.bottom + 4) + "px";
                    }
                } else {
                    dropdown.style.display = "none";
                }
            }
            event.stopImmediatePropagation();
            return;
        }
        // Then handle selectOption chip clicks (toggle selected state)
        const chip = target.closest('[data-type="selectOption"]') as HTMLElement;
        if (!chip) return;
        const path = chip.dataset.path;
        const row = getRow(chip);
        if (!path || !row) return;
        const dropdown = menuElement.querySelector(`[data-type="selectDropdown"][data-path="${path}"]`) as HTMLElement;
        const isSingle = dropdown?.dataset.single === "true";
        const useEl = chip.querySelector("use");
        const isCheck = useEl.getAttribute("xlink:href") === "#iconCheck";
        if (isSingle && !isCheck) {
            // Single select: when clicking a new option, first deselect all other selected options in this dropdown
            dropdown.querySelectorAll('[data-type="selectOption"]').forEach((c: HTMLElement) => {
                if (c !== chip) {
                    const u = c.querySelector("use");
                    if (u && u.getAttribute("xlink:href") === "#iconCheck") {
                        u.setAttribute("xlink:href", "#iconUncheck");
                        c.classList.remove("b3-chip--primary");
                    }
                }
            });
        }
        // Toggle the current item's iconCheck/iconUncheck + primary class
        useEl.setAttribute("xlink:href", isCheck ? "#iconUncheck" : "#iconCheck");
        chip.classList.toggle("b3-chip--primary", !isCheck);
        // Update the trigger display (rebuild the chip list, consistent with the table cell style)
        const triggerEl = menuElement.querySelector(`[data-type="selectTrigger"][data-path="${path}"]`) as HTMLElement;
        if (triggerEl && dropdown) {
            const isSingleSel = dropdown.dataset.single === "true";
            const placeholderStr = isSingleSel ? window.siyuan.languages.select : window.siyuan.languages.multiSelect;
            const selectedChips: string[] = [];
            dropdown.querySelectorAll('[data-type="selectOption"]').forEach((c: HTMLElement) => {
                const u = c.querySelector("use");
                if (u && u.getAttribute("xlink:href") === "#iconCheck") {
                    const name = c.dataset.name;
                    const color = c.dataset.color;
                    selectedChips.push(`<span class="b3-chip b3-chip--middle av__select-chip" style="background-color:var(--b3-font-background${color});color:var(--b3-font-color${color})">${escapeHtml(name)}</span>`);
                }
            });
            const contentHTML = selectedChips.join("") || `<span class="ft__on-surface fn__ellipsis">${placeholderStr}</span>`;
            triggerEl.innerHTML = contentHTML;
        }
        saveRow(row, path, false);
        event.stopImmediatePropagation();
    });

    // Clicking a blank area of the panel collapses all select dropdowns
    panelElement.addEventListener("click", (event: MouseEvent) => {
        const target = event.target as HTMLElement;
        if (!target.closest('[data-type="selectTrigger"]') && !target.closest('[data-type="selectDropdown"]')) {
            menuElement.querySelectorAll('[data-type="selectDropdown"]').forEach((el: HTMLElement) => {
                el.style.display = "none";
            });
        }
        if (!target.closest('[data-type-rel="relation"]') && !target.closest('[data-type="relList"]')) {
            menuElement.querySelectorAll('[data-type="relList"]').forEach((el: HTMLElement) => {
                el.style.display = "none";
            });
        }
    }, true);

    // select search filtering
    panelElement.addEventListener("input", (event: InputEvent) => {
        const target = event.target as HTMLElement;
        if (target.dataset.type === "filterSearch") {
            const path = target.dataset.path;
            // The dropdown panel is outside the row; look up options inside the dropdown by path
            const dropdown = menuElement.querySelector(`[data-type="selectDropdown"][data-path="${path}"]`);
            if (!dropdown) return;
            const key = (target as HTMLInputElement).value.toLowerCase();
            dropdown.querySelectorAll('[data-type="selectOption"]').forEach((chip: HTMLElement) => {
                const name = (chip.dataset.name || "").toLowerCase();
                chip.style.display = (!key || name.indexOf(key) > -1 || key.indexOf(name) > -1) ? "" : "none";
            });
        } else if (target.dataset.type === "filterValue" && target.dataset.typeRel === "relation") {
            // Relation filtering matches against the displayed primary key text; the input content serves as
            // both the candidate search keyword and the filter value.
            const path = target.dataset.path;
            const filter = getFilterByPath(getEditableFilters(data), path);
            const sourceColumn = findColData(path);
            const colData = filter && sourceColumn ? resolveFilterValueType(filter, sourceColumn).colData : sourceColumn;
            if (!colData?.relation?.avID) return;
            const keyword = (target as HTMLInputElement).value;
            fetchPost("/api/av/getAttributeViewPrimaryKeyValues", {
                id: colData.relation.avID,
                keyword,
            }, response => {
                if ((target as HTMLInputElement).value !== keyword) {
                    return;
                }
                const row = getRow(target);
                if (!row) return;
                let listEl = menuElement.querySelector(`[data-type="relList"][data-path="${path}"]`) as HTMLElement;
                if (!listEl) {
                    listEl = document.createElement("div");
                    listEl.setAttribute("data-type", "relList");
                    listEl.setAttribute("data-path", path);
                    listEl.className = "av__select-dropdown b3-list b3-list--background";
                    menuElement.appendChild(listEl);
                }
                let html = "";
                (response.data.rows.values as IAVCellValue[] || []).forEach((item, index) => {
                    const content = item.block?.content || window.siyuan.languages.untitled;
                    html += `<div class="b3-list-item${index === 0 ? " b3-list-item--focus" : ""}" data-path="${path}" data-name="${escapeAttr(content)}">${escapeHtml(content)}</div>`;
                });
                listEl.innerHTML = html;
                if (!html) {
                    listEl.style.display = "none";
                    return;
                }
                const rect = target.getBoundingClientRect();
                listEl.style.zIndex = (++window.siyuan.zIndex).toString();
                listEl.style.left = rect.left + "px";
                listEl.style.width = rect.width + "px";
                listEl.style.visibility = "hidden";
                listEl.style.display = "block";
                const listHeight = listEl.offsetHeight;
                listEl.style.visibility = "";
                listEl.style.top = window.innerHeight - rect.bottom < listHeight + 8 && rect.top > listHeight + 8
                    ? rect.top - listHeight - 4 + "px"
                    : rect.bottom + 4 + "px";
            });
        }
    });

    // Fill the value when a relation candidate is clicked
    panelElement.addEventListener("click", (event: MouseEvent) => {
        const target = event.target as HTMLElement;
        const item = target.closest('[data-type="relList"] .b3-list-item') as HTMLElement;
        if (!item) return;
        const listEl = item.closest('[data-type="relList"]') as HTMLElement;
        const path = listEl.dataset.path;
        const row = menuElement.querySelector(`.av__filter-row[data-path="${path}"]`) as HTMLElement;
        if (!path || !row) return;
        const input = row.querySelector('[data-type="filterValue"]') as HTMLInputElement;
        if (input) {
            input.value = item.dataset.name || "";
        }
        listEl.style.display = "none";
        saveRow(row, path, false);
    }, true); // capture, to avoid conflicting with the selectOption click
};
