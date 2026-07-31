import type {RowPart} from "../render/parts";
import type {SettingControl} from "./control";
import type {SettingGroup} from "./group";
import {getSettingGroupsByTabId} from "./group";
import {buildItemSearchIndex} from "../search/normalize";

type SettingItemBase = {
    id: string;
    tabId: string;
    groupId: string;
    /** Search strings of the item, normalized at registration time */
    searchIndex: readonly string[];
    readValue?: (el: HTMLElement) => unknown;
    save?: (value: unknown) => void | Promise<void>;
    afterMount?: (root: HTMLElement) => void | Promise<void>;
};

/** Standard control row: rowParts describe the whole row, it takes part in mount, save and search */
type FullSettingItem = SettingItemBase & {
    kind: "full";
    rowParts: RowPart[];
};

/** Custom HTML block: it takes part in mount and search */
type RenderSettingItem = SettingItemBase & {
    kind: "render";
    html: () => string;
    searchTexts?: () => string[];
};

/** Control embedded in a composite block: it only takes part in the readValue / save routing */
type BindingSettingItem = SettingItemBase & {
    kind: "binding";
    control: SettingControl;
};

type SettingItem = FullSettingItem | RenderSettingItem | BindingSettingItem;
export type MountableSettingItem = FullSettingItem | RenderSettingItem;
export type RegisterSettingItem =
    | Omit<FullSettingItem, "searchIndex">
    | Omit<RenderSettingItem, "searchIndex">
    | Omit<BindingSettingItem, "searchIndex">;

export type TabGroupEntry = {
    group: SettingGroup;
    items: MountableSettingItem[];
};

const settingItemsById = new Map<SettingItem["id"], SettingItem>();
const itemsByGroupCache = new Map<string, Map<string, MountableSettingItem[]>>();

const getMountableItemsByGroup = (tabId: string): Map<string, MountableSettingItem[]> => {
    let itemsByGroup = itemsByGroupCache.get(tabId);
    if (itemsByGroup) {
        return itemsByGroup;
    }
    itemsByGroup = new Map<string, MountableSettingItem[]>();
    for (const item of settingItemsById.values()) {
        if (item.kind !== "binding" && item.tabId === tabId) {
            const groupItems = itemsByGroup.get(item.groupId);
            if (groupItems) {
                groupItems.push(item);
            } else {
                itemsByGroup.set(item.groupId, [item]);
            }
        }
    }
    itemsByGroupCache.set(tabId, itemsByGroup);
    return itemsByGroup;
};

/** View of the items of a tab, grouped and in registration order, shared by rendering, search and mount */
export const getTabGroupEntries = (tabId: string): TabGroupEntry[] => {
    const itemsByGroup = getMountableItemsByGroup(tabId);
    return getSettingGroupsByTabId(tabId).map((group) => ({
        group,
        items: itemsByGroup.get(group.id) ?? [],
    }));
};

export const registerSettingItem = (item: RegisterSettingItem) => {
    settingItemsById.set(item.id, {
        ...item,
        searchIndex: buildItemSearchIndex(item)
    } as SettingItem);
    if (item.kind !== "binding") {
        itemsByGroupCache.delete(item.tabId);
    }
};

export const getSettingItem = (id: string) => settingItemsById.get(id);

/** Removes the items of a tab from the registry, so that a rebuild can register them again against the latest config */
export const removeSettingTabItems = (tabId: string) => {
    for (const [id, item] of settingItemsById) {
        if (item.tabId === tabId) {
            settingItemsById.delete(id);
        }
    }
    itemsByGroupCache.delete(tabId);
};
