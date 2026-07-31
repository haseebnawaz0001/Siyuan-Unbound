import {fetchSyncPost} from "../util/fetch";

import type {EventBus} from "./EventBus";

const KERNEL_PLUGIN_START_RETRY_INTERVAL = 100;
const KERNEL_PLUGIN_START_RETRY_COUNT = 50;

export class Kernel implements IKernelPlugin {
    public state: IKernelPluginState;
    public rpc: IKernelPluginRpc;

    /**
     * The app ID the kernel plugin belongs to, used to distinguish kernel plugins across different apps; it is
     * sent when establishing the WebSocket connection so the kernel can route messages correctly
     */
    #appId: string;

    /**
     * The name of the kernel plugin, which must match the name used when the kernel plugin registers itself;
     * used to establish the WebSocket connection and receive notifications
     */
    #name: string;

    /**
     * The client plugin's event bus
     */
    #eventBus: EventBus;

    /**
     * JSON-RPC WebSocket connection, used to receive notification-type RPC calls from the kernel plugin
     *
     * Regular RPC calls use POST requests
     */
    #rpcWs: WebSocket | null;

    /**
     * Handlers for the kernel plugin's notification-type RPC calls
     */
    #handlers: Map<TJsonRpcMethod, Set<TJsonRpcHandler<void>>>;

    #rpcCallUrl: string;

    constructor(options: {
        appId: string;
        name: string;
        eventBus: EventBus;
    }) {
        this.#appId = options.appId;
        this.#name = options.name;
        this.#eventBus = options.eventBus;

        this.#handlers = new Map();
        this.#rpcWs = null;
        this.#rpcCallUrl = this.#createJsonRpcCallUrl();

        this.state = this.#createState();
        this.rpc = this.#createRpc();
    }

    #createState(): IKernelPluginState {
        return new KernelState((state) => {
            switch (state.code) {
                case 2: // running
                    if (this.#rpcWs == null) {
                        this.#rpcWs = this.#createJsonRpcWebSocket();
                        break;
                    }

                    switch (this.#rpcWs.readyState) {
                        case WebSocket.CONNECTING:
                        case WebSocket.OPEN:
                            // The kernel plugin is running and the WebSocket connection is established or being
                            // established, so nothing needs to be done
                            break;
                        case WebSocket.CLOSING:
                        case WebSocket.CLOSED:
                            // The kernel plugin is already running, but the WebSocket connection is not
                            // established or has been disconnected; try to reconnect
                            this.#rpcWs = this.#createJsonRpcWebSocket();
                            break;
                    }
                    break;
            }
            this.#eventBus.emit("kernel-plugin-state-change", state);
        });
    }

    #createRpc(): IKernelPluginRpc {
        const call = new Proxy({} as Record<TJsonRpcMethod, (...args: TJsonRpcMethodParams) => Promise<any>>, {
            get: (_target, method: string) => {
                return (...args: TJsonRpcMethodParams) => this.#rpcCall(method, ...args);
            }
        });

        const notify = new Proxy({} as Record<TJsonRpcMethod, (...args: TJsonRpcMethodParams) => void>, {
            get: (_target, method: string) => {
                return (...args: TJsonRpcMethodParams) => this.#rpcNotify(method, ...args);
            }
        });

        return {
            call,
            notify,
            batch: this.#rpcBatchCall.bind(this),
            bind: (method: TJsonRpcMethod, handler: TJsonRpcHandler<void>) => {
                const handlers = (() => {
                    let handlers = this.#handlers.get(method);
                    if (!handlers) {
                        handlers = new Set();
                        this.#handlers.set(method, handlers);
                    }
                    return handlers;
                })();
                handlers.add(handler);
            },
            unbind: (method: TJsonRpcMethod, handler: TJsonRpcHandler<void>) => {
                const handlers = this.#handlers.get(method);
                if (handlers) {
                    handlers.delete(handler);
                    if (handlers.size === 0) {
                        this.#handlers.delete(method);
                    }
                }
            },
        };
    }

    #createJsonRpcWebSocket() {
        const websocketURL = new URL(window.location.origin);
        websocketURL.protocol = websocketURL.protocol === "https:" ? "wss:" : "ws:";
        websocketURL.pathname = "/ws/plugin/rpc";
        websocketURL.searchParams.set("name", this.#name);
        const ws = new WebSocket(websocketURL);
        ws.addEventListener("message", (event) => {
            const message = JSON.parse(event.data);
            // JSON-RPC notification: no id field, has a method field
            if (message.method && message.id === undefined) {
                const handlers = this.#handlers.get(message.method);
                if (handlers) {
                    const params = Array.isArray(message.params) ? message.params : [message.params];
                    handlers.forEach(async handler => {
                        try {
                            await handler(...params);
                        } catch (error) {
                            console.error(`Error handling JSON-RPC notification for method ${message.method}:`, error);
                        }
                    });
                }
            }
        });
        return ws;
    }

    #createJsonRpcCallUrl() {
        const searchParams = new URLSearchParams({ name: this.#name, appId: this.#appId });
        return `api/plugin/rpc?${searchParams.toString()}`;
    }

    #generateId(): TJsonRpcId {
        return Lute.NewNodeID();
    }

    async #fetchRpc(body: string): Promise<any> {
        for (let retry = 0; ; retry++) {
            const response = await fetch(this.#rpcCallUrl, {
                method: "POST",
                headers: { "Content-Type": "application/json" },
                body,
            });
            if (response.status === 204) {
                return;
            }

            const data = await response.json();
            const code = data.error?.code;
            if ((code !== -32001 && code !== -32002) || retry >= KERNEL_PLUGIN_START_RETRY_COUNT) {
                return data;
            }
            await new Promise(resolve => window.setTimeout(resolve, KERNEL_PLUGIN_START_RETRY_INTERVAL));
        }
    }

    async #initState() {
        const response = await fetchSyncPost("/api/plugin/getLoadedPlugin", { name: this.#name });
        if (this.state.code === -1 && response.data?.stateCode != null) {
            this.state.code = response.data.stateCode;
        }
    }

    async #rpcCall(method: TJsonRpcMethod, ...params: TJsonRpcMethodParams): Promise<any> {
        const id = this.#generateId();

        const data = await this.#fetchRpc(JSON.stringify({ jsonrpc: "2.0", id, method, params }));
        if (data.error) {
            throw new JsonRpcError(data.error);
        }
        return data.result;
    }

    #rpcNotify(method: TJsonRpcMethod, ...params: TJsonRpcMethodParams): void {
        this.#fetchRpc(JSON.stringify({ jsonrpc: "2.0", method, params })).catch((error) => {
            console.error(`Failed to send JSON-RPC notification for method ${method}:`, error);
        });
    }

    async #rpcBatchCall(...calls: IKernelPluginRpcCall[]): Promise<IKernelPluginRpcError | (IKernelPluginRpcResultResponse | IKernelPluginRpcErrorResponse)[]> {
        const requests = calls.map(call => {
            const request: IKernelPluginRpcRequest = { jsonrpc: "2.0", method: call.method };
            if (call.params != null) {
                request.params = call.params;
            }
            if (!call.notification) {
                request.id = call.id ?? this.#generateId();
            }
            return request;
        });

        return this.#fetchRpc(JSON.stringify(requests));
    }

    public async init() {
        try {
            await this.#initState();
        } catch (error) {
            console.error("Failed to initialize kernel plugin state:", error);
        }
    }

    public async destroy() {
        try {
            this.#rpcWs?.close();
        } catch (error) {
            console.error("Failed to close JSON-RPC WebSocket connection:", error);
        }
    }
}

export class KernelState implements IKernelPluginState {
    #code: TKernelPluginState = -1;
    #description: string = "inactive";

    #onchange: ((state: IKernelPluginState) => void);

    constructor(onchange: ((state: IKernelPluginState) => void)) {
        this.#onchange = onchange;
    }

    set code(state: TKernelPluginState) {
        this.#code = state;
        this.#description = (() => {
            switch (state) {
                case -1:
                    return "inactive";
                case 0:
                    return "ready";
                case 1:
                    return "loading";
                case 2:
                    return "running";
                case 3:
                    return "stopping";
                case 4:
                    return "stopped";
                case 5:
                    return "error";
                default:
                    return "unknown";
            }
        })();

        this.#onchange({
            code: this.#code,
            description: this.#description,
        });
    }

    get code() {
        return this.#code;
    }

    get description() {
        return this.#description;
    }
}

export class JsonRpcError extends Error implements IKernelPluginRpcError {
    code: number;
    data?: any;

    constructor(error: IKernelPluginRpcError) {
        super(error.message);

        this.code = error.code;
        this.data = error.data;
    }
}
