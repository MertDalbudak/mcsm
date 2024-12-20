#!/bin/sh

SCREEN_NAME="mc-server"

while getopts h:p:l: flag
do
    case "$flag" in
        h) # display Help
            echo "Use -p to define the path of the server";
            exit;;
        p) SERVER_PATH=$OPTARG;;
        \?) # Invalid option
            echo "Error: Invalid option"
    esac
done

if ! screen -list | grep -q $SCREEN_NAME; then
    echo "starting minecraft server";
    screen -dmS $SCREEN_NAME sh -c "cd $SERVER_PATH && java -Xms1G -Xmx3G -XX:+UseG1GC -XX:+ParallelRefProcEnabled -XX:MaxGCPauseMillis=200 -XX:+UnlockExperimentalVMOptions -XX:+DisableExplicitGC -XX:+AlwaysPreTouch -XX:G1NewSizePercent=30 -XX:G1MaxNewSizePercent=40 -XX:G1HeapRegionSize=8M -XX:G1ReservePercent=20 -XX:G1HeapWastePercent=5 -XX:G1MixedGCCountTarget=4 -XX:InitiatingHeapOccupancyPercent=15 -XX:G1MixedGCLiveThresholdPercent=90 -XX:G1RSetUpdatingPauseTimePercent=5 -XX:SurvivorRatio=32 -XX:+PerfDisableSharedMem -XX:MaxTenuringThreshold=1 -jar paper.jar nogui"
    screen -S $SCREEN_NAME -X multiuser on
    screen -S $SCREEN_NAME -X acladd root,$(whoami)
    echo "successful";
else
    echo "Minecraft server already running!"
fi
