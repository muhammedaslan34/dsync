export namespace discovery {
	
	export class LocalAddr {
	    ip: string;
	    kind: string;
	    interface: string;
	
	    static createFrom(source: any = {}) {
	        return new LocalAddr(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.ip = source["ip"];
	        this.kind = source["kind"];
	        this.interface = source["interface"];
	    }
	}

}

export namespace main {
	
	export class BackgroundSettings {
	    trayAvailable: boolean;
	    keepInTray: boolean;
	    autostartSupported: boolean;
	    autostart: boolean;
	    appMenuSupported: boolean;
	    appMenu: boolean;
	
	    static createFrom(source: any = {}) {
	        return new BackgroundSettings(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.trayAvailable = source["trayAvailable"];
	        this.keepInTray = source["keepInTray"];
	        this.autostartSupported = source["autostartSupported"];
	        this.autostart = source["autostart"];
	        this.appMenuSupported = source["appMenuSupported"];
	        this.appMenu = source["appMenu"];
	    }
	}
	export class FirewallStatus {
	    supported: boolean;
	    ruleOk: boolean;
	    publicNetworks: string[];
	    error?: string;
	
	    static createFrom(source: any = {}) {
	        return new FirewallStatus(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.supported = source["supported"];
	        this.ruleOk = source["ruleOk"];
	        this.publicNetworks = source["publicNetworks"];
	        this.error = source["error"];
	    }
	}
	export class UpdateInfo {
	    current: string;
	    latest?: string;
	    available: boolean;
	    url?: string;
	    notes?: string;
	    canInstall: boolean;
	    error?: string;
	
	    static createFrom(source: any = {}) {
	        return new UpdateInfo(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.current = source["current"];
	        this.latest = source["latest"];
	        this.available = source["available"];
	        this.url = source["url"];
	        this.notes = source["notes"];
	        this.canInstall = source["canInstall"];
	        this.error = source["error"];
	    }
	}

}

export namespace node {
	
	export class ClipboardStatus {
	    enabled: boolean;
	    available: boolean;
	    error?: string;
	
	    static createFrom(source: any = {}) {
	        return new ClipboardStatus(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.enabled = source["enabled"];
	        this.available = source["available"];
	        this.error = source["error"];
	    }
	}
	export class FileInfo {
	    name: string;
	    size: number;
	    path: string;
	    status: string;
	    error?: string;
	    transferId?: string;
	    folder?: boolean;
	    files?: number;
	    doneFiles?: number;
	    doneBytes?: number;
	    folderId?: string;
	    run?: string;
	
	    static createFrom(source: any = {}) {
	        return new FileInfo(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.name = source["name"];
	        this.size = source["size"];
	        this.path = source["path"];
	        this.status = source["status"];
	        this.error = source["error"];
	        this.transferId = source["transferId"];
	        this.folder = source["folder"];
	        this.files = source["files"];
	        this.doneFiles = source["doneFiles"];
	        this.doneBytes = source["doneBytes"];
	        this.folderId = source["folderId"];
	        this.run = source["run"];
	    }
	}
	export class HostInfo {
	    moonlight: boolean;
	    moonlightHint?: string;
	    sunshineInstalled: boolean;
	    sunshineRunning: boolean;
	    sunshineHint?: string;
	    sunshineLogin: boolean;
	    sunshineBlocked: boolean;
	    sunshineUrl: string;
	
	    static createFrom(source: any = {}) {
	        return new HostInfo(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.moonlight = source["moonlight"];
	        this.moonlightHint = source["moonlightHint"];
	        this.sunshineInstalled = source["sunshineInstalled"];
	        this.sunshineRunning = source["sunshineRunning"];
	        this.sunshineHint = source["sunshineHint"];
	        this.sunshineLogin = source["sunshineLogin"];
	        this.sunshineBlocked = source["sunshineBlocked"];
	        this.sunshineUrl = source["sunshineUrl"];
	    }
	}
	export class Message {
	    id: number;
	    time: number;
	    peerId: string;
	    peerName: string;
	    incoming: boolean;
	    text?: string;
	    file?: FileInfo;
	
	    static createFrom(source: any = {}) {
	        return new Message(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.id = source["id"];
	        this.time = source["time"];
	        this.peerId = source["peerId"];
	        this.peerName = source["peerName"];
	        this.incoming = source["incoming"];
	        this.text = source["text"];
	        this.file = this.convertValues(source["file"], FileInfo);
	    }
	
		convertValues(a: any, classs: any, asMap: boolean = false): any {
		    if (!a) {
		        return a;
		    }
		    if (a.slice && a.map) {
		        return (a as any[]).map(elem => this.convertValues(elem, classs));
		    } else if ("object" === typeof a) {
		        if (asMap) {
		            for (const key of Object.keys(a)) {
		                a[key] = new classs(a[key]);
		            }
		            return a;
		        }
		        return new classs(a);
		    }
		    return a;
		}
	}
	export class PairRequest {
	    id: string;
	    peerId: string;
	    name: string;
	    os: string;
	    code: string;
	
	    static createFrom(source: any = {}) {
	        return new PairRequest(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.id = source["id"];
	        this.peerId = source["peerId"];
	        this.name = source["name"];
	        this.os = source["os"];
	        this.code = source["code"];
	    }
	}
	export class Peer {
	    id: string;
	    name: string;
	    os: string;
	    addr: string;
	    manual: boolean;
	    online: boolean;
	    paired: boolean;
	    lastSeen: number;
	    lastHeard: number;
	    oneWay: boolean;
	
	    static createFrom(source: any = {}) {
	        return new Peer(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.id = source["id"];
	        this.name = source["name"];
	        this.os = source["os"];
	        this.addr = source["addr"];
	        this.manual = source["manual"];
	        this.online = source["online"];
	        this.paired = source["paired"];
	        this.lastSeen = source["lastSeen"];
	        this.lastHeard = source["lastHeard"];
	        this.oneWay = source["oneWay"];
	    }
	}

}

export namespace proto {
	
	export class Device {
	    id: string;
	    name: string;
	    os: string;
	    port: number;
	
	    static createFrom(source: any = {}) {
	        return new Device(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.id = source["id"];
	        this.name = source["name"];
	        this.os = source["os"];
	        this.port = source["port"];
	    }
	}

}

