import type {RowPart} from "../render/parts";

/** Normalizes a searchable text: strips HTML, trims and lowercases it */
export const normalizeSearchText = (text: string): string => {
    let plain = text || "";
    if (plain.includes("<")) {
        const el = document.createElement("div");
        el.innerHTML = plain;
        plain = el.textContent || "";
    }
    return plain.trim().toLowerCase();
};

/** Returns the keywords typed in the search box of the setting dialog */
export const getSearchKeywordsLower = (dialogElement: HTMLElement): string | undefined => {
    const searchInput = dialogElement.querySelector(".config__tab-head .b3-text-field") as HTMLInputElement | null;
    const keywords = normalizeSearchText(searchInput?.value ?? "");
    return keywords || undefined;
};

/** Builds the search index at registration time, normalizing and de-duplicating the strings */
export const buildSearchIndex = (rawStrings: readonly string[]): readonly string[] => {
    const strings: string[] = [];
    for (const text of rawStrings) {
        const normalized = normalizeSearchText(text);
        if (normalized.length > 0) {
            strings.push(normalized);
        }
    }
    return [...new Set(strings)];
};

/** Builds the search index of an item at registration time, normalizing and de-duplicating the strings */
export const buildItemSearchIndex = (item: {
    kind: string;
    rowParts?: RowPart[];
    searchTexts?: () => string[];
}): readonly string[] => {
    const strings: string[] = [];
    if (item.kind === "full" && item.rowParts) {
        for (const part of item.rowParts) {
            for (const s of collectPartSearchStrings(part)) {
                if (s.length > 0) {
                    strings.push(s);
                }
            }
        }
    }
    if (item.searchTexts) {
        for (const text of item.searchTexts()) {
            const normalized = normalizeSearchText(text);
            if (normalized.length > 0) {
                strings.push(normalized);
            }
        }
    }
    return [...new Set(strings)];
};

const collectPartSearchStrings = (part: RowPart): string[] => {
    switch (part.kind) {
        case "title":
        case "desc":
            return [normalizeSearchText(part.text)];
        case "select":
            return (part.options ?? []).map((o) => normalizeSearchText(o.label ?? String(o.value)));
        case "number":
            return part.unit ? [normalizeSearchText(part.unit)] : [];
        default:
            return [];
    }
};
