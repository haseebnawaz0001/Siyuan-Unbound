import {showMessage} from "../../dialog/message";
import {fetchPost, fetchSyncPost} from "../../util/fetch";
import {confirmDialog} from "../../dialog/confirmDialog";
import {isInIOS, saveExportFile} from "../../protyle/util/compatibility";
import {needSubscribe} from "../../util/needSubscribe";
import {getCloudURL} from "../util/about";

/**
 * Whether the configuration form for a user-supplied storage provider may be shown.
 * S3, WebDAV and the local filesystem sync to storage the user owns and never contact SiYuan's servers, so they need
 * no account and no subscription. Only the official provider (0) is account-gated.
 */
const userSuppliedStorageAllowed = (): boolean => true;

/** Refreshes the visibility and the dynamic panels of the sync tab from the current config, called from syncRuntime */
export const refreshSyncTabPanels = (root: Element) => {
    setSyncConfigItemVisible(root);
    setSyncModeRelatedConfigItemVisible(root);
    renderProviderConfig(root);
    renderCloudSpace(root);
};

/** Refreshes only the visibility of the setting items that depend on the sync mode, called from syncRuntime */
export const refreshSyncModeRelatedItems = (root: Element) => {
    setSyncModeRelatedConfigItemVisible(root);
};

const setSyncConfigItemVisible = (root: Element) => {
    const visible = window.siyuan.config.sync.provider === 0 ? !needSubscribe("") : userSuppliedStorageAllowed();
    [
        "cloudSpace",
        "sync.enabled",
        "sync.generateConflictDoc",
        "sync.mode",
        "sync.interval",
        "sync.perception",
        "syncCloudDirBlock",
        "syncCloudBackupBlock",
    ]
    .forEach((id) => {
        root.querySelector(`#${CSS.escape(id)}`)?.closest(".config-item")?.classList.toggle("fn__none", !visible);
    });
};

const setSyncModeRelatedConfigItemVisible = (root: Element) => {
    const syncModeElement = root.querySelector(`#${CSS.escape("sync.mode")}`) as HTMLSelectElement | null;
    if (!syncModeElement) {
        return;
    }
    const syncMode: Config.ISync["mode"] = Number(syncModeElement.value);
    const isProviderOfficialAutoSync = syncMode === 1 && !needSubscribe("");
    root.querySelector(`#${CSS.escape("sync.interval")}`)?.closest(".config-item")?.classList.toggle("fn__none", !isProviderOfficialAutoSync);
    root.querySelector(`#${CSS.escape("sync.perception")}`)?.closest(".config-item")?.classList.toggle("fn__none", !(isProviderOfficialAutoSync && window.siyuan.config.sync.provider === 0 && window.siyuan.config.system.container !== "docker"));
};

/** Search keywords of the sync provider config area, used when syncTab registers the slot */
export const getSyncProviderConfigKeywords = (): string[] => buildProviderConfigKeywords();

type SyncProviderConfigKey = Extract<keyof Config.ISync, "s3" | "webdav" | "local">;

type SyncProviderFieldDef =
    | {type: "input"; label: string; id: string; attrs?: string}
    | {type: "password"; label: string; id: string}
    | {type: "select"; label: string; id: string; options: {value: string; label: string}[]};

type SyncProviderIntroDef = {
    genIntro: () => string;
    // Only rendered when isProviderConfigAllowed() is false, which now happens for the official cloud alone.
    genUnpaidIntro?: () => string;
    isProviderConfigAllowed: () => boolean;
};

type SyncThirdPartyProviderDef = SyncProviderIntroDef & {
    configKey: SyncProviderConfigKey;
    api: string;
    getConfig: () => Config.ISync[SyncProviderConfigKey];
    fields: SyncProviderFieldDef[];
};

type SyncProviderDef = SyncProviderIntroDef | SyncThirdPartyProviderDef;

const isThirdPartySyncProviderDef = (def: SyncProviderDef): def is SyncThirdPartyProviderDef => {
    return "configKey" in def;
};

const SYNC_PROVIDER_DEFS: Record<Config.ISync["provider"], SyncProviderDef> = {
    0: {
        isProviderConfigAllowed: () => !needSubscribe(""),
        genIntro: () => `<div class="b3-label b3-label--inner">${window.siyuan.languages.syncOfficialProviderIntro}</div>`,
        genUnpaidIntro: () => {
            const accountServer = getCloudURL("");
            return `<div class="b3-label b3-label--inner">
    ${isInIOS() ? window.siyuan.languages._kernel[295] : window.siyuan.languages._kernel[29].replaceAll("${accountServer}", accountServer)}
</div>
<div class="b3-label b3-label--inner">
    ${window.siyuan.languages.cloudIntro1}
    <div class="b3-label__text">
        <ul class="fn__list">
            <li>${window.siyuan.languages.cloudIntro2}</li>
            <li>${window.siyuan.languages.cloudIntro3}</li>
            <li>${window.siyuan.languages.cloudIntro4}</li>
            <li>${window.siyuan.languages.cloudIntro5}</li>
            <li>${window.siyuan.languages.cloudIntro6}</li>
            <li>${window.siyuan.languages.cloudIntro7}</li>
            <li>${window.siyuan.languages.cloudIntro8}</li>
        </ul>
    </div>
</div>
<div class="b3-label b3-label--inner">
    ${window.siyuan.languages.cloudIntro9}
    <div class="b3-label__text">
        <ul style="padding-left: 2em">
            <li>${window.siyuan.languages.cloudIntro10}</li>
            <li>${window.siyuan.languages.cloudIntro11}</li>
        </ul>
    </div>
</div>`;
        },
    },
    2: {
        isProviderConfigAllowed: userSuppliedStorageAllowed,
        configKey: "s3",
        api: "/api/sync/setSyncProviderS3",
        getConfig: () => window.siyuan.config.sync.s3,
        genIntro: () => `<div class="b3-label b3-label--inner">
    ${window.siyuan.languages.syncThirdPartyProviderS3Intro}
    <div class="fn__hr"></div>
    ${window.siyuan.languages.syncThirdPartyProviderTip}
</div>`,
        fields: [
            {type: "input", label: "Endpoint", id: "endpoint"},
            {type: "input", label: "Access Key", id: "accessKey"},
            {type: "password", label: "Secret Key", id: "secretKey"},
            {type: "input", label: "Bucket", id: "bucket"},
            {type: "input", label: "Region ID", id: "region"},
            {type: "input", label: "Timeout (s)", id: "timeout", attrs: 'type="number" min="7" max="300"'},
            {type: "select", label: "Addressing", id: "pathStyle", options: [
                {value: "true", label: "Path-style"},
                {value: "false", label: "Virtual-hosted-style"},
            ]},
            {type: "select", label: "TLS Verify", id: "skipTlsVerify", options: [
                {value: "false", label: "Verify"},
                {value: "true", label: "Skip"},
            ]},
            {type: "input", label: "Concurrent Reqs", id: "concurrentReqs", attrs: 'type="number" min="1" max="16"'},
        ],
    },
    3: {
        isProviderConfigAllowed: userSuppliedStorageAllowed,
        configKey: "webdav",
        api: "/api/sync/setSyncProviderWebDAV",
        getConfig: () => window.siyuan.config.sync.webdav,
        genIntro: () => `<div class="b3-label b3-label--inner">
    ${window.siyuan.languages.syncThirdPartyProviderWebDAVIntro}
    <div class="fn__hr"></div>
    ${window.siyuan.languages.syncThirdPartyProviderTip}
</div>`,
        fields: [
            {type: "input", label: "Endpoint", id: "endpoint"},
            {type: "input", label: "Username", id: "username"},
            {type: "password", label: "Password", id: "password"},
            {type: "input", label: "Timeout (s)", id: "timeout", attrs: 'type="number" min="7" max="300"'},
            {type: "select", label: "TLS Verify", id: "skipTlsVerify", options: [
                {value: "false", label: "Verify"},
                {value: "true", label: "Skip"},
            ]},
            {type: "input", label: "Concurrent Reqs", id: "concurrentReqs", attrs: 'type="number" min="1" max="16"'},
        ],
    },
    4: {
        isProviderConfigAllowed: userSuppliedStorageAllowed,
        configKey: "local",
        api: "/api/sync/setSyncProviderLocal",
        getConfig: () => window.siyuan.config.sync.local,
        genIntro: () => `<div class="b3-label b3-label--inner">
    <div class="ft__error">
        ${window.siyuan.languages.mobileNotSupport}
    </div>
    <div class="fn__hr"></div>
    ${window.siyuan.languages.syncThirdPartyProviderLocalIntro}
</div>`,
        fields: [
            {type: "input", label: "Endpoint", id: "endpoint"},
            {type: "input", label: "Timeout (s)", id: "timeout", attrs: 'type="number" min="7" max="300"'},
            {type: "input", label: "Concurrent Reqs", id: "concurrentReqs", attrs: 'type="number" min="1" max="1024"'},
        ],
    },
};

const buildProviderConfigKeywords = (): string[] => {
    const accountServer = getCloudURL("");
    return [
        // Official cloud (provider === 0)
        window.siyuan.languages.syncOfficialProviderIntro,
        window.siyuan.languages._kernel[29].replaceAll("${accountServer}", accountServer),
        window.siyuan.languages._kernel[295],
        window.siyuan.languages.cloudIntro1,
        window.siyuan.languages.cloudIntro2,
        window.siyuan.languages.cloudIntro3,
        window.siyuan.languages.cloudIntro4,
        window.siyuan.languages.cloudIntro5,
        window.siyuan.languages.cloudIntro6,
        window.siyuan.languages.cloudIntro7,
        window.siyuan.languages.cloudIntro8,
        window.siyuan.languages.cloudIntro9,
        window.siyuan.languages.cloudIntro10,
        window.siyuan.languages.cloudIntro11,
        // Hints shown when not subscribed, when running locally and so on
        window.siyuan.languages.mobileNotSupport,
        // S3 / WebDAV / local third party providers
        window.siyuan.languages.syncThirdPartyProviderS3Intro,
        window.siyuan.languages.syncThirdPartyProviderWebDAVIntro,
        window.siyuan.languages.syncThirdPartyProviderLocalIntro,
        window.siyuan.languages.syncThirdPartyProviderTip,
        // Action buttons
        window.siyuan.languages.cloudStoragePurge,
        window.siyuan.languages.import,
        window.siyuan.languages.export,
        // Form labels and options, hardcoded in English
        "Endpoint",
        "Access Key",
        "Secret Key",
        "Bucket",
        "Region ID",
        "Timeout (s)",
        "Addressing",
        "Path-style",
        "Virtual-hosted-style",
        "TLS Verify",
        "Verify",
        "Skip",
        "Concurrent Reqs",
        "Username",
        "Password",
    ];
};

const renderProviderConfig = (root: Element) => {
    const providerConfigElement = root.querySelector("#syncProviderConfig");
    if (!providerConfigElement) {
        return;
    }

    const def = SYNC_PROVIDER_DEFS[window.siyuan.config.sync.provider];
    let html = "";
    if (def) {
        if (!def.isProviderConfigAllowed()) {
            html = def.genUnpaidIntro?.() ?? "";
        } else if (isThirdPartySyncProviderDef(def)) {
            html = `${def.genIntro()}${def.fields.map(genProviderField).join("")}${genProviderActionButtons(def.configKey)}`;
        } else {
            html = def.genIntro();
        }
    }

    providerConfigElement.innerHTML = html;
    bindProviderConfigEvent(providerConfigElement, root);
};

const genProviderField = (field: SyncProviderFieldDef): string => {
    switch (field.type) {
        case "input":
            return genProviderFlexInput(field.label, field.id, field.attrs);
        case "password":
            return genProviderFlexPassword(field.label, field.id);
        case "select":
            return genProviderFlexSelect(field.label, field.id, field.options.map((option) => `
    <option value="${option.value}">${option.label}</option>`).join(""));
    }
};

const genProviderFlexInput = (label: string, id: string, attrs = "") => `<div class="b3-label b3-label--inner fn__flex">
    <div class="fn__flex-center fn__size200">${label}</div>
    <div class="fn__space"></div>
    <input id="${id}" class="b3-text-field fn__block"${attrs ? ` ${attrs}` : ""}>
</div>`;

const genProviderFlexPassword = (label: string, id: string) => `<div class="b3-label b3-label--inner fn__flex">
    <div class="fn__flex-center fn__size200">${label}</div>
    <div class="fn__space"></div>
    <div class="b3-form__icona fn__block">
        <input id="${id}" type="password" class="b3-text-field b3-form__icona-input">
        <svg class="b3-form__icona-icon" data-action="togglePassword"><use xlink:href="#iconEye"></use></svg>
    </div>
</div>`;

const genProviderFlexSelect = (label: string, id: string, optionsHtml: string) => `<div class="b3-label b3-label--inner fn__flex">
    <div class="fn__flex-center fn__size200">${label}</div>
    <div class="fn__space"></div>
    <select class="b3-select fn__block" id="${id}">
        ${optionsHtml}
    </select>
</div>`;

const genProviderActionButtons = (dataType: SyncProviderConfigKey) => {
    const importExportHtml = dataType === "s3" || dataType === "webdav" ? `<div class="fn__space"></div>
    <button class="b3-button b3-button--outline fn__size200" style="position: relative">
        <input id="importSyncConfig" class="b3-form__upload" type="file" data-type="${dataType}">
        <svg><use xlink:href="#iconDownload"></use></svg>${window.siyuan.languages.import}
    </button>
    <div class="fn__space"></div>
    <button class="b3-button b3-button--outline fn__size200" id="exportSyncConfig" data-type="${dataType}">
        <svg><use xlink:href="#iconUpload"></use></svg>${window.siyuan.languages.export}
    </button>` : "";
    return `<div class="b3-label b3-label--inner fn__flex fn__flex-wrap">
    <div class="fn__flex-1"></div>
    <button class="b3-button b3-button--outline fn__size200" id="purgeCloudData">
        <svg><use xlink:href="#iconTrashcan"></use></svg>${window.siyuan.languages.cloudStoragePurge}
    </button>${importExportHtml}
</div>`;
};

const syncProviderConfigBoundElements = new WeakSet<Element>();

const bindProviderConfigEvent = (configElement: Element, root: Element) => {
    const togglePasswordIcon = configElement.querySelector('[data-action="togglePassword"]');
    togglePasswordIcon?.addEventListener("click", () => {
        const useElement = togglePasswordIcon.firstElementChild;
        const isEye = useElement.getAttribute("xlink:href") === "#iconEye";
        useElement.setAttribute("xlink:href", isEye ? "#iconEyeoff" : "#iconEye");
        (togglePasswordIcon.previousElementSibling as HTMLInputElement).setAttribute("type", isEye ? "text" : "password");
    });

    const importElement = configElement.querySelector("#importSyncConfig") as HTMLInputElement;
    importElement?.addEventListener("change", () => {
        const formData = new FormData();
        formData.append("file", importElement.files[0]);
        const isS3 = importElement.getAttribute("data-type") === "s3";
        fetchPost(isS3 ? "/api/sync/importSyncProviderS3" : "/api/sync/importSyncProviderWebDAV", formData, (response) => {
            if (isS3) {
                window.siyuan.config.sync.s3 = response.data.s3;
            } else {
                window.siyuan.config.sync.webdav = response.data.webdav;
            }
            renderProviderConfig(root);
            showMessage(window.siyuan.languages.imported);
        });
    });

    const exportButton = configElement.querySelector("#exportSyncConfig");
    exportButton?.addEventListener("click", () => {
        fetchPost(exportButton.getAttribute("data-type") === "s3" ? "/api/sync/exportSyncProviderS3" : "/api/sync/exportSyncProviderWebDAV", {}, (response) => {
            void saveExportFile(response.data.zip);
        });
    });

    configElement.querySelector("#purgeCloudData")?.addEventListener("click", () => {
        confirmDialog("♻️ " + window.siyuan.languages.cloudStoragePurge, `<div class="b3-typography">${window.siyuan.languages.cloudStoragePurgeConfirm}</div>`, () => {
            fetchPost("/api/repo/purgeCloudRepo");
        });
    });

    const provider = window.siyuan.config.sync.provider;
    const def = SYNC_PROVIDER_DEFS[provider];
    if (!isThirdPartySyncProviderDef(def) || !def.isProviderConfigAllowed()) {
        return;
    }
    fillSyncProviderConfigValues(configElement);
    if (syncProviderConfigBoundElements.has(configElement)) {
        return;
    }
    syncProviderConfigBoundElements.add(configElement);
    configElement.addEventListener("change", (event: Event) => {
        const target = event.target as HTMLElement;
        if (!target.matches(".b3-text-field, .b3-select")) {
            return;
        }
        saveSyncProviderConfigValues(configElement);
    });
};

const saveSyncProviderConfigValues = (configElement: Element) => {
    const provider = window.siyuan.config.sync.provider;
    const def = SYNC_PROVIDER_DEFS[provider];
    if (!isThirdPartySyncProviderDef(def)) {
        return;
    }
    const data = readProviderConfigFields(configElement, def.getConfig());
    const configKey = def.configKey;
    // fetchSyncPost is used because fetchPost skips the callback when the kernel returns code < 0, while the UI always has to be written back to match the saved config
    fetchSyncPost(def.api, {[configKey]: data})
        .then((response) => {
            if (response.code === 0 && response.data?.[configKey]) {
                window.siyuan.config.sync[configKey] = response.data[configKey];
            }
        })
        .finally(() => {
            fillSyncProviderConfigValues(configElement);
        })
        .catch(() => {});
};

const fillSyncProviderConfigValues = (configElement: Element) => {
    const provider = window.siyuan.config.sync.provider;
    const def = SYNC_PROVIDER_DEFS[provider];
    if (!isThirdPartySyncProviderDef(def)) {
        return;
    }
    const data = def.getConfig();
    (Object.keys(data) as (keyof typeof data & string)[]).forEach((key) => {
        const el = configElement.querySelector(`#${key}`) as HTMLInputElement | HTMLSelectElement | null;
        if (el) {
            el.value = String(data[key]);
        }
    });
};

const readProviderConfigFields = <T extends object>(configElement: Element, template: T): T => {
    const result = {} as Record<string, unknown>;
    (Object.keys(template) as (keyof T & string)[]).forEach((key) => {
        const el = configElement.querySelector(`#${key}`) as HTMLInputElement | HTMLSelectElement | null;
        if (!el) {
            return;
        }
        const sample = template[key];
        if (typeof sample === "boolean") {
            result[key] = el.value === "true";
        } else if (typeof sample === "number") {
            result[key] = parseInt(el.value, 10);
        } else {
            result[key] = el.value;
        }
    });
    return result as T;
};

const renderCloudSpace = (root: Element) => {
    const cloudSpaceElement = root.querySelector("#cloudSpace");
    if (!cloudSpaceElement) {
        return;
    }

    const isProviderOfficial = window.siyuan.config.sync.provider === 0;
    const subscribed = !needSubscribe("");
    const hidden = cloudSpaceElement.classList.toggle("fn__none", !isProviderOfficial || !subscribed);
    if (!hidden) {
        cloudSpaceElement.innerHTML = buildCloudSpaceHtml(
            Object.fromEntries(CLOUD_SPACE_DISPLAY_KEYS.map((key) => [key, "0B"])) as CloudSpaceDisplayData,
            true
        );
        fetchSyncPost("/api/cloud/getCloudSpace").then((response) => {
            if (response.code === 1) {
                cloudSpaceElement.innerHTML = `<span class="ft__error">${response.msg}</span>`;
                return;
            }
            if (response.code !== 0 || !response.data) {
                return;
            }
            cloudSpaceElement.innerHTML = buildCloudSpaceHtml({
                syncSize: response.data.sync.hSize,
                backupSize: response.data.backup.hSize,
                hAssetSize: response.data.hAssetSize,
                hSize: response.data.hSize,
                hTotalSize: response.data.hTotalSize,
                hExchangeSize: response.data.hExchangeSize,
                hTrafficUploadSize: response.data.hTrafficUploadSize,
                hTrafficDownloadSize: response.data.hTrafficDownloadSize,
                hTrafficAPIGet: response.data.hTrafficAPIGet,
                hTrafficAPIPut: response.data.hTrafficAPIPut,
            }, false);
        }).catch(() => {});
    }
};

const CLOUD_SPACE_DISPLAY_KEYS = [
    "syncSize",
    "backupSize",
    "hAssetSize",
    "hSize",
    "hTotalSize",
    "hExchangeSize",
    "hTrafficUploadSize",
    "hTrafficDownloadSize",
    "hTrafficAPIGet",
    "hTrafficAPIPut",
] as const;

type CloudSpaceDisplayData = Record<(typeof CLOUD_SPACE_DISPLAY_KEYS)[number], string>;

const buildCloudSpaceHtml = (data: CloudSpaceDisplayData, loading: boolean) =>
    `<div class="fn__flex config-cloud-space${loading ? " config-cloud-space--loading" : ""}">
    <div class="config-cloud-space__body">
        ${window.siyuan.languages.cloudStorage}
        <div class="config-cloud-space__placeholder">
        <div class="fn__hr"></div>
        <ul class="b3-list">
            <li class="b3-list-item" style="cursor: auto;">${window.siyuan.languages.sync}<span class="b3-list-item__meta">${data.syncSize}</span></li>
            <li class="b3-list-item" style="cursor: auto;">${window.siyuan.languages.backup}<span class="b3-list-item__meta">${data.backupSize}</span></li>
            <li class="b3-list-item" style="cursor: auto;"><a href="${getCloudURL("settings/file?type=3")}" target="_blank">${window.siyuan.languages.cdn}</a><span class="b3-list-item__meta">${data.hAssetSize}</span></li>
            <li class="b3-list-item" style="cursor: auto;">${window.siyuan.languages.total}<span class="b3-list-item__meta">${data.hSize}</span></li>
            <li class="b3-list-item" style="cursor: auto;">${window.siyuan.languages.sizeLimit}<span class="b3-list-item__meta">${data.hTotalSize}</span></li>
            <li class="b3-list-item" style="cursor: auto;"><a href="${getCloudURL("settings/point")}" target="_blank">${window.siyuan.languages.pointExchangeSize}</a><span class="b3-list-item__meta">${data.hExchangeSize}</span></li>
        </ul>
        </div>
    </div>
    <div class="config-cloud-space__body">
        ${window.siyuan.languages.trafficStat}
        <div class="config-cloud-space__placeholder">
        <div class="fn__hr"></div>
        <ul class="b3-list">
            <li class="b3-list-item" style="cursor: auto;">${window.siyuan.languages.upload}<span class="fn__space"></span><span class="ft__on-surface">${data.hTrafficUploadSize}</span></li>
            <li class="b3-list-item" style="cursor: auto;">${window.siyuan.languages.download}<span class="fn__space"></span><span class="ft__on-surface">${data.hTrafficDownloadSize}</span></li>
            <li class="b3-list-item" style="cursor: auto;">API GET<span class="fn__space"></span><span class="ft__on-surface">${data.hTrafficAPIGet}</span></li>
            <li class="b3-list-item" style="cursor: auto;">API PUT<span class="fn__space"></span><span class="ft__on-surface">${data.hTrafficAPIPut}</span></li>
        </ul>
        </div>
    </div>
    ${loading ? '<div class="fn__loading"><img width="64px" src="/stage/loading-pure.svg"></div>' : ""}
</div>`;
