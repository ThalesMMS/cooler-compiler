class A {
  same() : SELF_TYPE { self };
};
class B inherits A {};
class Main inherits IO {
  value : Int;
  main() : Object {
    let integer : Int, text : String, flag : Bool, item : A <- new B in {
      value <- integer;
      if flag then out_string("bad") else out_string(text.concat("defaults")) fi;
      case item of
        child : B => out_string("/B");
        parent : A => out_string("/A");
      esac;
      item.same();
    }
  };
};
