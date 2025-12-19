export namespace main {
	
	export class EmotePosition {
	    Start: number;
	    End: number;
	
	    static createFrom(source: any = {}) {
	        return new EmotePosition(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.Start = source["Start"];
	        this.End = source["End"];
	    }
	}
	export class EmoteInfo {
	    ID: string;
	    Name: string;
	    URL: string;
	    FilePath: string;
	    ImageURL: string;
	    Positions: EmotePosition[];
	
	    static createFrom(source: any = {}) {
	        return new EmoteInfo(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.ID = source["ID"];
	        this.Name = source["Name"];
	        this.URL = source["URL"];
	        this.FilePath = source["FilePath"];
	        this.ImageURL = source["ImageURL"];
	        this.Positions = this.convertValues(source["Positions"], EmotePosition);
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
	
	export class Message {
	    Username: string;
	    Content: string;
	    Channel: string;
	    Tags: Record<string, string>;
	    RawData: string;
	    // Go type: time
	    Timestamp: any;
	    Height: number;
	    UserColor: string;
	
	    static createFrom(source: any = {}) {
	        return new Message(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.Username = source["Username"];
	        this.Content = source["Content"];
	        this.Channel = source["Channel"];
	        this.Tags = source["Tags"];
	        this.RawData = source["RawData"];
	        this.Timestamp = this.convertValues(source["Timestamp"], null);
	        this.Height = source["Height"];
	        this.UserColor = source["UserColor"];
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
	export class TwitchConfig {
	    nickname: string;
	    oauthToken: string;
	    FilterList: string[];
	    RecordingEnabled: boolean;
	    ArchiveDir: string;
	    TTSPath: string;
	    TTSMessage: string;
	
	    static createFrom(source: any = {}) {
	        return new TwitchConfig(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.nickname = source["nickname"];
	        this.oauthToken = source["oauthToken"];
	        this.FilterList = source["FilterList"];
	        this.RecordingEnabled = source["RecordingEnabled"];
	        this.ArchiveDir = source["ArchiveDir"];
	        this.TTSPath = source["TTSPath"];
	        this.TTSMessage = source["TTSMessage"];
	    }
	}

}

