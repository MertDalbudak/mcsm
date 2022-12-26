const exec = require('child_process').exec;
const Paper = {
    ServerExecutable: "/paper.jar",
    ServerUpdateHost: "papermc.io",
    ServerUpdatePath: "/api/v1/paper/1.17/latest/download"
};

Paper.newVersionAvailable = function (){
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

Paper.getCurrentVersion = function (){
    console.log("Checking for paper updates...");
    return new Promise((resolve, reject)=>{
        exec("screen -S minecraft -X stuff 'version\n'", ()=>{
            this.once("logChange", ()=> {
                setTimeout(()=>{
                    const last_line = this.logLastLinesSync(6);
                    let version_text = last_line.match(/(MC: [0-9]+.[0-9]+.[0-9]+)/);
                    if(version_text != null){
                        resolve(version_text[0]);
                    }
                }, 3000)
            });
        });
    });
}

Paper.checkPlayerList = function(){
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

module.exports = Paper;