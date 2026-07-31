/** Reads a value by dotted path, following the same convention as the dots in a control `id` */
export function getAtPath(root: unknown, dottedPath: string): unknown {
    const segments = dottedPath.split(".");
    let cur: unknown = root;
    for (const s of segments) {
        if (cur === null || cur === undefined) {
            return undefined;
        }
        cur = (cur as Record<string, unknown>)[s];
    }
    return cur;
}

/**
 * Merges a leaf value into a config object by dotted path, shallow copying the root and then descending immutably.
 * Used by the setting tabs to merge a single item by control id, reading the DOM itself is left to each panel.
 */
function assignPathImmutable(
    obj: Record<string, unknown>,
    segments: string[],
    value: unknown
): Record<string, unknown> {
    if (segments.length === 1) {
        return {...obj, [segments[0]]: value};
    }
    const [head, ...rest] = segments;
    const child = obj[head];
    const base =
        typeof child === "object" && child !== null && !Array.isArray(child)
            ? {...(child as Record<string, unknown>)}
            : {};
    return {
        ...obj,
        [head]: assignPathImmutable(base, rest, value),
    };
}

/** Merges a leaf value into any string keyed config object, shallow copying the root before writing along the path */
export function mergeRecordByDottedPath<T extends Record<string, unknown>>(
    base: T,
    dottedId: string,
    value: unknown
): T {
    const segments = dottedId.split(".");
    return assignPathImmutable({...(base as unknown as Record<string, unknown>)}, segments, value) as T;
}
