import {fetchPost} from "../../util/fetch";
import {processSync} from "../../dialog/processSystem";
import {
    onSetaccount,
    updateAccountSwitchesVisibility,
} from "./accountUi";
import {
    refreshSyncModeRelatedItems,
    refreshSyncTabPanels,
} from "./syncUi";

/** Root node of the account and sync tab */
export let syncTabElement: HTMLElement | undefined;

/** Releases the reference to the tab root once the setting dialog is closed, so that no detached DOM is kept alive */
export const clearSyncTabElement = () => {
    syncTabElement = undefined;
};

/** Extra initialization of the account and sync tab, called after the registry has mounted it */
export const mountSyncTabExtras = (root: HTMLElement) => {
    syncTabElement = root;
    refreshSyncTabPanels(root);
    updateAccountSwitchesVisibility(root);
};

/** Refreshes the cloud space blocks and resets the cloud directory list, for example after switching the sync provider */
export const refreshSyncCloudSpaceGroup = (root: Element) => {
    refreshSyncTabPanels(root);
    const syncConfigElement = root.querySelector("#syncCloudList");
    if (syncConfigElement) {
        syncConfigElement.innerHTML = "";
        syncConfigElement.classList.add("fn__none");
    }
};

/** Account and sync tab: submits the config by control id and updates the local runtime */
export const patchSyncConfig = (controlId: string, value: unknown) => {
    switch (controlId) {
        case "account.displayTitle": {
            const displayTitle = Boolean(value) as Config.IAccount["displayTitle"];
            fetchPost("/api/setting/setAccount", {
                ...window.siyuan.config.account,
                displayTitle,
            }, (response) => {
                window.siyuan.config.account = response.data;
                onSetaccount();
            });
            break;
        }
        case "account.displayVIP": {
            const displayVIP = Boolean(value) as Config.IAccount["displayVIP"];
            fetchPost("/api/setting/setAccount", {
                ...window.siyuan.config.account,
                displayVIP,
            }, (response) => {
                window.siyuan.config.account = response.data;
                onSetaccount();
            });
            break;
        }

        case "sync.provider": {
            const provider = value as Config.ISync["provider"];
            fetchPost("/api/sync/setSyncProvider", {provider}, () => {
                window.siyuan.config.sync.provider = provider;
                if (syncTabElement) {
                    refreshSyncCloudSpaceGroup(syncTabElement);
                }
            });
            break;
        }
        case "sync.enabled": {
            const enabled = Boolean(value) as Config.ISync["enabled"];
            fetchPost("/api/sync/setSyncEnable", {enabled}, () => {
                window.siyuan.config.sync.enabled = enabled;
                processSync();
            });
            break;
        }
        case "sync.generateConflictDoc": {
            const generateConflictDoc = Boolean(value) as Config.ISync["generateConflictDoc"];
            fetchPost("/api/sync/setSyncGenerateConflictDoc", {enabled: generateConflictDoc}, () => {
                window.siyuan.config.sync.generateConflictDoc = generateConflictDoc;
            });
            break;
        }
        case "sync.mode": {
            const mode = value as Config.ISync["mode"];
            fetchPost("/api/sync/setSyncMode", {mode}, () => {
                window.siyuan.config.sync.mode = mode;
                if (syncTabElement) {
                    refreshSyncModeRelatedItems(syncTabElement);
                }
            });
            break;
        }
        case "sync.interval": {
            const interval = value as Config.ISync["interval"];
            fetchPost("/api/sync/setSyncInterval", {interval}, () => {
                window.siyuan.config.sync.interval = interval;
                processSync();
            });
            break;
        }
        case "sync.perception": {
            const perception = Boolean(value) as Config.ISync["perception"];
            fetchPost("/api/sync/setSyncPerception", {enabled: perception}, () => {
                window.siyuan.config.sync.perception = perception;
                processSync();
            });
            break;
        }

        case "repo.indexRetentionDays": {
            const indexRetentionDays = value as Config.IRepo["indexRetentionDays"];
            fetchPost("/api/repo/setRepoIndexRetentionDays", {days: indexRetentionDays}, () => {
                window.siyuan.config.repo.indexRetentionDays = indexRetentionDays;
            });
            break;
        }
        case "repo.retentionIndexesDaily": {
            const retentionIndexesDaily = value as Config.IRepo["retentionIndexesDaily"];
            fetchPost("/api/repo/setRetentionIndexesDaily", {indexes: retentionIndexesDaily}, () => {
                window.siyuan.config.repo.retentionIndexesDaily = retentionIndexesDaily;
            });
            break;
        }
        default:
            console.warn(`[config] patchSyncConfig: unhandled controlId "${controlId}"`);
            break;
    }
};
