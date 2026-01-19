export namespace main {
	
	export class SessionData {
	    id?: string;
	    active: boolean;
	    paused?: boolean;
	    taskGuid: string;
	    taskTitle: string;
	    taskSource?: string;
	    elapsed: number;
	    startedAt?: string;
	    endedAt?: string;
	
	    static createFrom(source: any = {}) {
	        return new SessionData(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.id = source["id"];
	        this.active = source["active"];
	        this.paused = source["paused"];
	        this.taskGuid = source["taskGuid"];
	        this.taskTitle = source["taskTitle"];
	        this.taskSource = source["taskSource"];
	        this.elapsed = source["elapsed"];
	        this.startedAt = source["startedAt"];
	        this.endedAt = source["endedAt"];
	    }
	}
	export class DashboardStats {
	    date: string;
	    planned: number;
	    completed: number;
	    queued: number;
	    total_time: number;
	
	    static createFrom(source: any = {}) {
	        return new DashboardStats(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.date = source["date"];
	        this.planned = source["planned"];
	        this.completed = source["completed"];
	        this.queued = source["queued"];
	        this.total_time = source["total_time"];
	    }
	}
	export class HabitData {
	    id: string;
	    name: string;
	    icon: string;
	    type: string;
	    done: boolean;
	    progress: number;
	
	    static createFrom(source: any = {}) {
	        return new HabitData(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.id = source["id"];
	        this.name = source["name"];
	        this.icon = source["icon"];
	        this.type = source["type"];
	        this.done = source["done"];
	        this.progress = source["progress"];
	    }
	}
	export class DashboardEvent {
	    id: string;
	    title: string;
	    start: string;
	    end?: string;
	    duration?: string;
	    location?: string;
	
	    static createFrom(source: any = {}) {
	        return new DashboardEvent(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.id = source["id"];
	        this.title = source["title"];
	        this.start = source["start"];
	        this.end = source["end"];
	        this.duration = source["duration"];
	        this.location = source["location"];
	    }
	}
	export class DashboardTask {
	    guid: string;
	    title: string;
	    source: string;
	    status: string;
	    estimate?: string;
	    scheduledTime?: string;
	    elapsedToday?: number;
	    sessionCount?: number;
	
	    static createFrom(source: any = {}) {
	        return new DashboardTask(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.guid = source["guid"];
	        this.title = source["title"];
	        this.source = source["source"];
	        this.status = source["status"];
	        this.estimate = source["estimate"];
	        this.scheduledTime = source["scheduledTime"];
	        this.elapsedToday = source["elapsedToday"];
	        this.sessionCount = source["sessionCount"];
	    }
	}
	export class DashboardData {
	    tasks: DashboardTask[];
	    events: DashboardEvent[];
	    habits: HabitData[];
	    stats: DashboardStats;
	    session?: SessionData;
	    sessions?: SessionData[];
	
	    static createFrom(source: any = {}) {
	        return new DashboardData(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.tasks = this.convertValues(source["tasks"], DashboardTask);
	        this.events = this.convertValues(source["events"], DashboardEvent);
	        this.habits = this.convertValues(source["habits"], HabitData);
	        this.stats = this.convertValues(source["stats"], DashboardStats);
	        this.session = this.convertValues(source["session"], SessionData);
	        this.sessions = this.convertValues(source["sessions"], SessionData);
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
	
	
	
	

}

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

