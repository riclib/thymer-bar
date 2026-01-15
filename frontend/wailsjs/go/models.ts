export namespace ui {
	
	export class PluginInfo {
	    ID: string;
	    Name: string;
	    Description: string;
	    Type: string;
	    Installed: boolean;
	    InstalledVer: number;
	    AvailableVer: number;
	    AutoUpdate: boolean;
	    Status: string;
	    GUID: string;
	    HasLocalFiles: boolean;
	
	    static createFrom(source: any = {}) {
	        return new PluginInfo(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.ID = source["ID"];
	        this.Name = source["Name"];
	        this.Description = source["Description"];
	        this.Type = source["Type"];
	        this.Installed = source["Installed"];
	        this.InstalledVer = source["InstalledVer"];
	        this.AvailableVer = source["AvailableVer"];
	        this.AutoUpdate = source["AutoUpdate"];
	        this.Status = source["Status"];
	        this.GUID = source["GUID"];
	        this.HasLocalFiles = source["HasLocalFiles"];
	    }
	}

}

