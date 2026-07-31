import {buildGroupedItemsView} from "../render/render";
import {getSearchKeywordsLower, normalizeSearchText} from "../search/normalize";
import {Constants} from "../../constants";
import type {SettingTabMountContext} from "./builder";
import {getSettingTab, type TSettingTab} from "./tabs";

/** First mount: renders every registered item and runs their afterMount hooks */
export const mountSettingTab = async (tabId: string, root: HTMLElement) => {
    const {html, items} = buildGroupedItemsView(tabId);
    root.innerHTML = html;

    for (const item of items) {
        await item.afterMount?.(root);
    }
};

/** Registers the items again and replaces the whole page, for when the setting dialog is open and the tab is mounted */
export const remountOpenSettingTab = async (tabId: TSettingTab) => {
    const dialogElement = window.siyuan.dialogs.find((d) => d.element.getAttribute("data-key") === Constants.DIALOG_SETTING)?.element;
    if (!dialogElement) {
        return;
    }
    const root = dialogElement.querySelector(`.config__tab-container[data-name="${tabId}"]`) as HTMLElement | null;
    if (!root?.innerHTML) {
        return;
    }
    const search: Partial<SettingTabMountContext> = {};
    const keywords = getSearchKeywordsLower(dialogElement);
    const tab = getSettingTab(tabId);
    if (keywords) {
        const result = tab.scanSearch(keywords);
        search.keywords = keywords;
        search.visibleItemIds = result.visibleItemIds;
        search.visibleGroupIds = result.visibleGroupIds;
    }
    await tab.mount(root, search, undefined, true);
};

export const applySettingTabSearchVisibility = (
    root: HTMLElement,
    visibleItemIds: Set<string>,
    visibleGroupIds: Set<string>,
) => {
    root.querySelectorAll("[data-config-group-id]").forEach((groupEl) => {
        const groupId = groupEl.getAttribute("data-config-group-id");
        const groupVisible = groupId && visibleGroupIds.has(groupId);
        groupEl.classList.toggle("config-search-hidden", !groupVisible);
        if (!groupVisible) {
            return;
        }

        let lastVisibleItem: Element | null = null;
        groupEl.querySelectorAll("[data-config-item-id]").forEach((itemEl) => {
            itemEl.classList.remove("config-item--last-visible");
            const itemId = itemEl.getAttribute("data-config-item-id");
            const itemVisible = itemId && visibleItemIds.has(itemId);
            itemEl.classList.toggle("config-search-hidden", !itemVisible);
            if (itemVisible) {
                lastVisibleItem = itemEl;
            }
        });
        // Mark the last item of each group that is not hidden, so that it gets no border-bottom
        lastVisibleItem?.classList.add("config-item--last-visible");
    });
};

export const clearSettingTabSearch = (root: HTMLElement) => {
    root.querySelectorAll("[data-config-group-id], [data-config-item-id]").forEach((el) => {
        el.classList.remove("config-search-hidden", "config-item--last-visible");
    });
};

/** Panel style SettingTab: switches the layout-tab-bar sub tab according to the global search keywords */
export const switchSettingPanelSubTab = (
    root: HTMLElement,
    keywords: string,
    subTabs: {type: string; label: string}[],
) => {
    const focusedType = root.querySelector(".layout-tab-bar .item--focus")?.getAttribute("data-type");
    const focusedTab = subTabs.find((t) => t.type === focusedType);
    if (focusedTab && normalizeSearchText(focusedTab.label).includes(keywords)) {
        return;
    }
    for (const tab of subTabs) {
        if (!normalizeSearchText(tab.label).includes(keywords)) {
            continue;
        }
        const tabItem = root.querySelector(`.layout-tab-bar .item[data-type="${tab.type}"]`) as HTMLElement | null;
        if (tabItem && !tabItem.classList.contains("item--focus")) {
            tabItem.click();
        }
        break;
    }
};
