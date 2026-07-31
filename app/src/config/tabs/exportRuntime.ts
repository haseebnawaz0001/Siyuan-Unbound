import {createConfigNamespaceApi} from "../util/namespaceApi";

const applyExportConfig = (data: Config.IExport) => {
    window.siyuan.config.export = data;
    const pathDisplay = document.getElementById("pandocBinPathDisplay");
    if (pathDisplay) {
        pathDisplay.textContent = data.pandocBin;
    }
};

/** Export config namespace, used as the save handler of the items registered in the setting panel and to bind the buttons inside stacks */
export const exportConfigApi = createConfigNamespaceApi<Config.IExport>({
    namespace: "export",
    getConfig: () => window.siyuan.config.export,
    setConfig: applyExportConfig,
    apiPath: "/api/setting/setExport",
});
