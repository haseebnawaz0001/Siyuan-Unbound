/// #if MOBILE
import {saveScroll} from "../../protyle/scroll/saveScroll";
/// #else
import {adjustDockPadding} from "../../layout/dock/util";
import {exportLayout} from "../../layout/util";
import {syncHideToolbarLayout, updateBarModeIcon} from "../../layout/topBar";
/// #endif
import {fetchPost} from "../../util/fetch";
import {loadAssets} from "../../util/assets";
import {remountOpenSettingTab} from "../setting/mount";
import {createConfigNamespaceApi} from "../util/namespaceApi";

/** Initial value of the theme mode select, combining mode and modeOS */
export const appearanceThemeModeValue = (): number =>
    window.siyuan.config.appearance.modeOS ? 2 : window.siyuan.config.appearance.mode;

/** Theme mode selection, split back into mode and modeOS before it is submitted */
export const saveThemeMode = (value: number) => {
    const OSThemeMode = window.matchMedia("(prefers-color-scheme: dark)").matches ? 1 : 0;
    fetchPost("/api/setting/setAppearance", {
        ...window.siyuan.config.appearance,
        mode: (value === 2 ? OSThemeMode : value) as Config.IAppearance["mode"],
        modeOS: value === 2,
    });
};

/// #if MOBILE
const reloadUI = async () => {
    if (window.siyuan.mobile.editor) {
        await saveScroll(window.siyuan.mobile.editor.protyle);
    }
    window.location.reload();
};
/// #endif

const applyAppearanceConfig = async (data: Config.IAppearance) => {
    if (data.lang !== window.siyuan.config.appearance.lang) {
        /// #if MOBILE
        void reloadUI();
        /// #else
        void exportLayout({
            cb() {
                window.location.reload();
            },
            errorExit: false,
        });
        /// #endif
        return;
    }

    if (window.siyuan.config.appearance.themeJS) {
        if (data.mode !== window.siyuan.config.appearance.mode ||
            (data.mode === window.siyuan.config.appearance.mode && (
                (data.mode === 0 && window.siyuan.config.appearance.themeLight !== data.themeLight) ||
                (data.mode === 1 && window.siyuan.config.appearance.themeDark !== data.themeDark))
            )
        ) {
            if (window.destroyTheme) {
                try {
                    await window.destroyTheme();
                    window.destroyTheme = undefined;
                    document.getElementById("themeScript").remove();
                } catch (e) {
                    console.error("destroyTheme error: " + e);
                }
            } else {
                /// #if MOBILE
                void reloadUI();
                /// #else
                void exportLayout({
                    errorExit: false,
                    cb() {
                        window.location.reload();
                    },
                });
                /// #endif
                return;
            }
        }
    }

    const prevAppearance = window.siyuan.config.appearance;
    window.siyuan.config.appearance = data;

    document.getElementById("status")?.classList.toggle("fn__none", data.hideStatusBar);
    /// #if !MOBILE
    if (data.hideStatusBar !== prevAppearance.hideStatusBar) {
        adjustDockPadding();
    }
    if (data.hideToolbar !== prevAppearance.hideToolbar) {
        syncHideToolbarLayout();
    }
    updateBarModeIcon();
    /// #endif

    loadAssets(data);
    /// #if !MOBILE
    void remountOpenSettingTab("appearance");
    /// #endif
};

/** Appearance config namespace, used as the save handler of the items registered in the setting panel */
export const appearanceConfigApi = createConfigNamespaceApi<Config.IAppearance>({
    namespace: "appearance",
    getConfig: () => window.siyuan.config.appearance,
    setConfig: (data) => {
        void applyAppearanceConfig(data);
    },
    apiPath: "/api/setting/setAppearance",
    applyFromResponse: false,
});
