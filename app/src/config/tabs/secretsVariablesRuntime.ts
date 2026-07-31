import {createConfigNamespaceApi} from "../util/namespaceApi";

/** Secrets store config namespace, used as the save handler of the items registered in the setting panel */
export const secretsConfigApi = createConfigNamespaceApi<Config.ISecrets>({
    namespace: "secrets",
    getConfig: () => window.siyuan.config.secrets,
    setConfig: (data) => {
        window.siyuan.config.secrets = data;
    },
    apiPath: "/api/setting/setSecrets",
});

/** Variables store config namespace, used as the save handler of the items registered in the setting panel */
export const variablesConfigApi = createConfigNamespaceApi<Config.IVariables>({
    namespace: "variables",
    getConfig: () => window.siyuan.config.variables,
    setConfig: (data) => {
        window.siyuan.config.variables = data;
    },
    apiPath: "/api/setting/setVariables",
});
