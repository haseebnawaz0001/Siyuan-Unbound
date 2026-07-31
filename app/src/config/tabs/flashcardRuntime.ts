import {createConfigNamespaceApi} from "../util/namespaceApi";

/** Flashcard config namespace, used as the save handler of the items registered in the setting panel */
export const flashcardConfigApi = createConfigNamespaceApi<Config.IFlashCard>({
    namespace: "flashcard",
    getConfig: () => window.siyuan.config.flashcard,
    setConfig: (data) => {
        window.siyuan.config.flashcard = data;
    },
    apiPath: "/api/setting/setFlashcard",
});
