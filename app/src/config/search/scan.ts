import {SettingTabSearchResult} from "../setting/builder";
import {getTabGroupEntries} from "../setting/item";

/** Walks the groups and items of a SettingTab once, yielding both the sidebar match and the visibility of the content area */
export const scanSettingTabSearch = (
    tabId: string,
    tabSearchTitle: string,
    keywords: string,
): SettingTabSearchResult => {
    const visibleItemIds = new Set<string>();
    const visibleGroupIds = new Set<string>();

    if (tabSearchTitle.length > 0 && tabSearchTitle.includes(keywords)) {
        // Matched the tab title
        for (const {group, items} of getTabGroupEntries(tabId)) {
            visibleGroupIds.add(group.id);
            for (const item of items) {
                visibleItemIds.add(item.id);
            }
        }
        return {matches: true, visibleItemIds, visibleGroupIds};
    }

    let matches = false;
    for (const {group, items} of getTabGroupEntries(tabId)) {
        if (group.searchTitle.length > 0 && group.searchTitle.includes(keywords)) {
            // Matched the group title
            matches = true;
            visibleGroupIds.add(group.id);
            for (const item of items) {
                visibleItemIds.add(item.id);
            }
            continue;
        }
        for (const item of items) {
            if (item.searchIndex.some((s) => s.includes(keywords))) {
                // Matched the text of the setting item
                matches = true;
                visibleItemIds.add(item.id);
                visibleGroupIds.add(group.id);
            }
        }
    }
    return {matches, visibleItemIds, visibleGroupIds};
};
