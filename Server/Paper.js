const {exec} = require('child_process');
const fs = require('fs/promises')
const Server = require('./Server');


/*
config is expected to look like this:
{
    "id": 0,
    "bin": "minecraft2 -n",
    "path": "/location/of/server",
    "logPath": "/logs/latest.log",
    "owner": "John Doe",
    "type": "Paper",
    "version": "1.19.3",
    "discord": {
        "token": "[DISCORD_TOKEN]",
        "channels":[
            {
                "id": "[DISCORD_CHANNEL_ID]",
                "name": "minecraft-activities"
            }
        ]
    },
    "ServerLogCheckInterval": 20
}

*/
class Paper extends Server {
    constructor(id){
        super(id);
        this.ServerExecutable ="/paper.jar";
        this.ServerUpdateHost = "papermc.io";
        this.ServerUpdatePath = "/api/v1/paper/1.19.3/latest/download";
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