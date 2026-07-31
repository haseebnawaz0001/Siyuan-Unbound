export const addScriptSync = async (path: string, id: string) => {
    if (document.getElementById(id)) {
        return false;
    }
    const xhrObj = new XMLHttpRequest();
    xhrObj.open("GET", path, false);
    xhrObj.setRequestHeader("Accept",
        "text/javascript, application/javascript, application/ecmascript, application/x-ecmascript, */*; q=0.01");
    xhrObj.send("");
    const scriptElement = document.createElement("script");
    scriptElement.type = "text/javascript";
    scriptElement.text = xhrObj.responseText;
    scriptElement.id = id;
    document.head.appendChild(scriptElement);
    if (typeof Lute === "undefined") {
        // On HarmonyOS, the first load can leave Lute undefined; reloading once fixes it. The root cause hasn't
        // been found yet, so this workaround is used for now.
        window.location.reload();
    }
};

export const addScript = (path: string, id: string) => {
    return new Promise((resolve) => {
        if (document.getElementById(id)) {
            // Return immediately if called again after the script has already loaded
            resolve(false);
            return false;
        }
        const scriptElement = document.createElement("script");
        scriptElement.src = path;
        scriptElement.async = true;
        // Chrome won't re-request the js when called repeatedly in a loop
        document.head.appendChild(scriptElement);
        scriptElement.onload = () => {
            if (document.getElementById(id)) {
                // The script tag must be removed from the DOM when called repeatedly in a loop
                scriptElement.remove();
                resolve(false);
                return false;
            }
            scriptElement.id = id;
            resolve(true);
        };
    });
};
