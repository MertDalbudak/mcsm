const {exec} = require('child_process');
const fs = require('fs/promises')
const Server = require('./Server');


class Paper extends Server { 
    constructor(id){
        super(id);
        super.ServerExecutable = "/paper.jar";
        super.ServerUpdateHost = "papermc.io";
        super.ServerUpdatePath = "/api/v1/paper/1.19.3/latest/download";
    }
    newVersionAvailable(){
        console.log("Checking for paper updates...");
        return new Promise((resolve, reject)=>{
            this.handler.input('version', ()=>{
                this.once("logChange", ()=> {
                    setTimeout(()=>{
                        const last_line = this.logLastLinesSync(10);
                        console.log(last_line);
                        resolve(last_line.match("[0-9]* version\\(s\\) behind") !== null);
                    }, 2500)
                });
            });
        });
    }
    async getBanlist(){
        console.log("Get banlist...");
        try{
            const banlist_file = await fs.readFile(this.banned_players_path, {'encoding': "utf-8"});
            const banned_players = JSON.parse(banlist_file);
            return banned_players;  
        }
        catch(error){
            console.error(error);
            throw new Error("Failed to retrieve banned player list");
        }
    }
}

module.exports = Paper;