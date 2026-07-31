import {fetchPost} from "../../util/fetch";
import {mergeRecordByDottedPath} from "./dotPath";

export function createConfigNamespaceApi<TData>(options: {
    namespace: string;
    getConfig: () => TData;
    setConfig: (data: TData) => void;
    apiPath: string;
    /** When true (the default) the response data is applied to the local config after a successful POST, when false the kernel is relied on to push the change to every frontend instance */
    applyFromResponse?: boolean;
}): {
    /**
     * @param onApplied Called after a successful POST with the namespace config returned by the API, which has the same shape as `getConfig()`.
     * When `applyFromResponse` is true, `setConfig` has already run before this call, when it is false the local `getConfig()` may not be in sync yet.
     */
    patch: (relOrFullId: string, value: unknown, onApplied?: (data: TData) => void) => void;
    apply: (data: TData) => void;
} {
    const {namespace, getConfig, setConfig, apiPath, applyFromResponse = true} = options;
    const prefix = `${namespace}.`;

    const post = (payload: TData, onApplied?: (data: TData) => void) => {
        fetchPost(apiPath, payload, (response) => {
            const data = response.data as TData;
            if (applyFromResponse) {
                // The kernel does not push this setting to the other frontend instances, so update the local config from the response
                setConfig(data);
            }
            onApplied?.(data);
        });
    };

    return {
        patch(relOrFullId, value, onApplied) {
            const rel = relOrFullId.startsWith(prefix) ? relOrFullId.slice(prefix.length) : relOrFullId;
            if (rel) {
                const prev = getConfig() as unknown as Record<string, unknown>;
                post(mergeRecordByDottedPath(prev, rel, value) as unknown as TData, onApplied);
            }
        },
        apply: setConfig,
    };
}
