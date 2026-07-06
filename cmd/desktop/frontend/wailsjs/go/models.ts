export namespace main {
	
	export class DesktopSettings {
	    fixedPort: number;
	    allowLAN: boolean;
	    currentPort: number;
	    currentBindHost: string;
	    settingsPath: string;
	    restartRequired: boolean;
	
	    static createFrom(source: any = {}) {
	        return new DesktopSettings(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.fixedPort = source["fixedPort"];
	        this.allowLAN = source["allowLAN"];
	        this.currentPort = source["currentPort"];
	        this.currentBindHost = source["currentBindHost"];
	        this.settingsPath = source["settingsPath"];
	        this.restartRequired = source["restartRequired"];
	    }
	}

}

