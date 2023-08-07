package module

//
//import (
//	"context"
//	"fmt"
//	"github.com/tiny-systems/module/pkg/api/module-go"
//	//"github.com/tiny-systems/module/pkg/discovery"
//	m "github.com/tiny-systems/module/pkg/module"
//	"github.com/tiny-systems/module/pkg/module/instance"
//)
//
//func (s *Server) InstallComponent(info m.Info, c m.Component) error {
//	s.installComponentsCh <- &installComponentMsg{
//		component: c,
//		module:    info,
//	}
//	return nil
//}
//
//func (s *Server) RunInstance(conf *module.ConfigureInstanceRequest) {
//	s.newInstanceCh <- instance.NewMsgWithSubject(conf.ComponentID, conf, func(data interface{}) error {
//		return nil
//	})
//}
//
//// spinNewInstance async to avoid slow consumer
//func (s *Server) spinNewInstance(ctx context.Context, runConfigMsg *instance.Msg, cmp *installComponentMsg, inputCh chan *instance.Msg, outputCh chan *instance.Msg) error {
//	// make new instance discoverable
//	// deploy instance
//	// create component runner
//	componentInstance := cmp.component.Instance()
//
//	if httpService, ok := componentInstance.(m.HTTPService); ok {
//		httpService.HTTPService(func(port int) (string, error) {
//			fmt.Println("exchange port", port)
//			return "https://node12.workspace.tinysystems.dev", nil
//		})
//	}
//
//	instanceRunner := instance.NewRunner(componentInstance, cmp.module).
//		SetLogger(s.log)
//
//	// deploy
//	runCtx, runCancel := context.WithCancel(ctx)
//	defer runCancel()
//
//	waitCh, err := instanceRunner.Run(runCtx, runConfigMsg, inputCh, outputCh)
//	if err != nil {
//		return err
//	}
//
//	<-waitCh
//	return nil
//}
