export namespace node {
	
	export class Message {
	    id: number;
	    time: number;
	    peerId: string;
	    peerName: string;
	    incoming: boolean;
	    text: string;
	
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
	    }
	}
	export class Peer {
	    id: string;
	    name: string;
	    os: string;
	    addr: string;
	    manual: boolean;
	    online: boolean;
	    lastSeen: number;
	
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
	        this.lastSeen = source["lastSeen"];
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

