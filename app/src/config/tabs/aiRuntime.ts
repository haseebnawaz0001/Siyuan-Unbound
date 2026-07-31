import {createConfigNamespaceApi} from "../util/namespaceApi";

/** AI config namespace, used as the save handler of the items registered in the setting panel */
export const aiConfigApi = createConfigNamespaceApi<Config.IAI>({
    namespace: "ai",
    getConfig: () => window.siyuan.config.ai,
    setConfig: (data) => {
        window.siyuan.config.ai = data;
    },
    apiPath: "/api/setting/setAI",
});
