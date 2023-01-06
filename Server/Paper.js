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
    constructor(config){
        super(config);
        this.ServerExecutable ="/paper.jar";
        this.ServerUpdateHost = "papermc.io";
        this.ServerUpdatePath = "/api/v1/paper/1.19.3/latest/download";
    }
    newVersionAvailable(){
        console.log("Checking for paper updates...");
        return new Promise((resolve, reject)=>{
            exec("screen -S minecraft -X stuff 'version\n'", ()=>{
                this.once("logChange", ()=> {
                    setTimeout(()=>{
                        const last_line = this.logLastLinesSync(10);
                        console.log(last_line);
                        resolve(last_line.match("[0-9]* version\\(s\\) behind") !== null);
                    }, 3500)
                });
            });
        });
    }
    getCurrentVersion(){
        console.log("Checking for paper updates...");
        return new Promise((resolve, reject)=>{
            exec("screen -S minecraft -X stuff 'version\n'", ()=>{
                this.once("logChange", ()=> {
                    setTimeout(()=>{
                        const last_line = this.logLastLinesSync(6);
                        let version_text = last_line.match(/(MC: [0-9]+.[0-9]+.[0-9]+)|(MC: [0-9]+.[0-9]+)/);
                        if(version_text != null){
                            resolve(version_text[0]);
                        }
                    }, 3000)
                });
            });
        });
    }
    getPlayerList(){
        console.log("Get concurrent online player count...");
        return new Promise((resolve, reject)=>{
            exec("screen -S minecraft -X stuff 'list\n'", ()=>{
                this.once("logChange", ()=> {
                    setTimeout(()=>{
                        const last_line = this.logLastLinesSync(1);
                        console.log(last_line);
                        if(last_line.match("(There are [0-9]+ of a max of [0-9]+ players online:)(.)*") !== null){
                            let player_list = last_line.split('online:')[1].trim().split(', ').filter(e => e != "");
                            resolve(player_list);
                        }
                        else{
                            reject("List command didn't yield any valuable information");
                        }
                        resolve();
                    }, 30)
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