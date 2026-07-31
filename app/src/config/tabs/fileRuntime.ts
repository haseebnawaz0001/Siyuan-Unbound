import {createConfigNamespaceApi} from "../util/namespaceApi";

/** Doc tree config namespace, used as the save handler of the items registered in the setting panel */
export const fileConfigApi = createConfigNamespaceApi<Config.IFileTree>({
    namespace: "fileTree",
    getConfig: () => window.siyuan.config.fileTree,
    setConfig: (data) => {
        window.siyuan.config.fileTree = data;
    },
    apiPath: "/api/setting/setFiletree",
});
