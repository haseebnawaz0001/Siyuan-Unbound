import {createConfigNamespaceApi} from "../util/namespaceApi";

/** Search config namespace, used as the save handler of the items registered in the setting panel */
export const searchConfigApi = createConfigNamespaceApi<Config.ISearch>({
    namespace: "search",
    getConfig: () => window.siyuan.config.search,
    setConfig: (data) => {
        window.siyuan.config.search = data;
    },
    apiPath: "/api/setting/setSearch",
});
